// Package install plans and performs ReShade installations into game
// directories, and removes them again using the manifest recorded at
// install time.
package install

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// Flavor selects the ReShade build.
type Flavor string

// Flavor values.
const (
	FlavorNormal Flavor = "normal"
	FlavorAddon  Flavor = "addon"
)

// Addon reports whether this flavor supports add-ons.
func (f Flavor) Addon() bool { return f == FlavorAddon }

// Directory and file names created inside a game directory.
const (
	ShadersRoot  = "reshade-shaders"
	ShadersDir   = ShadersRoot + "/Shaders"
	TexturesDir  = ShadersRoot + "/Textures"
	ININame      = "ReShade.ini"
	PresetName   = "ReShadePreset.ini"
	LogName      = "ReShade.log"
	BackupSuffix = ".yarm-bak"
	// removedSuffix marks a file an upgrade has superseded but not yet
	// committed to deleting. Distinct from BackupSuffix so the two can
	// never be confused during rollback.
	removedSuffix = ".yarm-old"
)

// Request describes an installation the user has asked for.
type Request struct {
	Game game.Game
	// Exe is the target executable, relative to the game root.
	Exe game.Executable
	// Version is a ReShade version such as "6.8.0".
	Version string
	Flavor  Flavor
	// DLLName is the proxy DLL to install, e.g. "dxgi.dll".
	DLLName string
	// Packages, Addons and Custom hold catalog / custom content ids.
	Packages []string
	Addons   []string
	Custom   []string
	// Overwrite allows replacing files YARM does not own, after backing
	// them up. Without it, such files are left alone and reported.
	Overwrite bool
	// TargetOS decides OS-specific rules, chiefly whether
	// d3dcompiler_47.dll is part of the install. It is a field rather than
	// a runtime.GOOS read so both branches are testable anywhere.
	TargetOS artifacts.TargetOS
}

// ExeDir returns the directory holding the target executable, relative to
// the game root. ReShade loads its DLL, add-ons and ini from here.
func (r Request) ExeDir() string {
	dir := path.Dir(filepath.ToSlash(r.Exe.Path))
	if dir == "." {
		return ""
	}
	return dir
}

// Validate checks a request is coherent before any planning happens.
func (r Request) Validate() error {
	switch {
	case r.Game.Root == "":
		return fmt.Errorf("game root is required")
	case r.Exe.Path == "":
		return fmt.Errorf("target executable is required")
	case r.Version == "":
		return fmt.Errorf("reshade version is required")
	case r.DLLName == "":
		return fmt.Errorf("dll name is required")
	case r.Flavor != FlavorNormal && r.Flavor != FlavorAddon:
		return fmt.Errorf("flavor must be %q or %q, got %q", FlavorNormal, FlavorAddon, r.Flavor)
	case len(r.Addons) > 0 && !r.Flavor.Addon():
		// ReShade's normal build does not load add-ons at all, so this
		// would silently install files that never activate.
		return fmt.Errorf("add-ons require the %q flavor", FlavorAddon)
	}
	if !strings.EqualFold(filepath.Ext(r.DLLName), ".dll") {
		return fmt.Errorf("dll name %q must end in .dll", r.DLLName)
	}
	if r.DLLName != filepath.Base(r.DLLName) {
		return fmt.Errorf("dll name %q must not contain a path", r.DLLName)
	}
	return nil
}

// Action is what the executor will do with one planned file.
type Action string

// Action values.
const (
	// ActionCreate writes a file that does not exist yet.
	ActionCreate Action = "create"
	// ActionReplace overwrites a file a previous YARM install created.
	ActionReplace Action = "replace"
	// ActionSkip leaves an identical file already in place.
	ActionSkip Action = "skip"
	// ActionBackup overwrites a file YARM does not own, saving the
	// original alongside first.
	ActionBackup Action = "backup"
	// ActionKeep leaves a foreign file untouched because Overwrite was
	// not given.
	ActionKeep Action = "keep"
)

// Writes reports whether an action puts a file on disk.
func (a Action) Writes() bool { return a == ActionCreate || a == ActionReplace || a == ActionBackup }

// PlannedFile is one file the install will place in the game directory.
type PlannedFile struct {
	// Source is an absolute path in the cache, or "" for generated
	// content such as ReShade.ini.
	Source string
	// Dest is relative to the game root, slash-separated.
	Dest   string
	Origin state.Origin
	Action Action
	// Size is the source's size, when known.
	Size int64
	// Owned is true when a previous install already recorded this
	// destination, so it is ours to keep managing.
	Owned bool
	// OwnedRecord is that previous manifest entry, valid when Owned is
	// true. It is carried forward rather than re-hashed so that a file the
	// user has edited since is still recognizable as edited.
	OwnedRecord state.File
}

// StepKind groups plan steps for progress reporting.
type StepKind string

// StepKind values.
const (
	StepDownload StepKind = "download"
	StepRemove   StepKind = "remove"
	StepBackup   StepKind = "backup"
	StepCopy     StepKind = "copy"
	StepINI      StepKind = "ini"
	StepManifest StepKind = "manifest"
)

// Step is one unit of work, shown in the review and progress screens.
type Step struct {
	Kind        StepKind
	Description string
}

// Plan is the result of planning an install.
type Plan struct {
	Request Request
	Steps   []Step
	Files   []PlannedFile
	// Removed lists files from a previous install of the same executable
	// that this one no longer needs; the executor deletes them first.
	Removed []string
	// Warnings are things the user should see before confirming, such as
	// foreign files that will be left in place.
	Warnings []string
	// Upgrade is set when an install already exists for this executable.
	Upgrade bool
}

// WriteCount returns how many files the plan will actually write.
func (p Plan) WriteCount() int {
	n := 0
	for _, f := range p.Files {
		if f.Action.Writes() {
			n++
		}
	}
	return n
}

// TotalBytes returns the size of everything the plan will write.
func (p Plan) TotalBytes() int64 {
	var n int64
	for _, f := range p.Files {
		if f.Action.Writes() {
			n += f.Size
		}
	}
	return n
}
