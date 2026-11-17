package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/secato/yarm/internal/install"
)

// HumanSize formats a byte count for display.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// FolderLabel names a game folder the way the lists do: with a trailing
// slash, and "(game root)/" for the game's own folder.
func FolderLabel(dir string) string { return folderLabel(dir) }

// RunOps applies each operation in turn, stopping at the first failure or
// cancellation.
//
// Each op is atomic on its own — the executor rolls itself back — but the
// batch is not, so the outcomes record exactly how far it got. Pressing on
// after a failure would mean writing into more folders after something
// already went wrong, which is not what anyone wants from a tool whose whole
// point is being able to undo itself. send may be called from any goroutine
// the installer uses.
func RunOps(ctx context.Context, inst Installer, un UninstallRunner, ops []Op, send func(ProgressUpdate)) []Outcome {
	// Every op gets an outcome up front, so the ones never reached still
	// name their folder rather than vanishing from the report.
	outcomes := make([]Outcome, len(ops))
	for i, op := range ops {
		outcomes[i].Op = op
	}
	narrate := func(op Op, label string) string {
		// Prefix a progress line with its folder only when there is more
		// than one to tell apart.
		if len(ops) < 2 || label == "" {
			return label
		}
		return folderLabel(op.Dir) + " " + label
	}

	for i, op := range ops {
		if ctx.Err() != nil {
			break
		}
		outcomes[i].Attempted = true

		switch {
		case op.Uninstall != nil:
			send(ProgressUpdate{Label: narrate(op, "Removing ReShade")})
			outcomes[i].Removed, outcomes[i].Err = un.Uninstall(*op.Uninstall)
		default:
			outcomes[i].Result, outcomes[i].Err = inst.Install(ctx, *op.Install, func(u ProgressUpdate) {
				u.Label = narrate(op, u.Label)
				send(u)
			})
		}

		if outcomes[i].Err != nil {
			break
		}
	}
	return outcomes
}

// Status is how one folder's share of an apply ended.
type Status string

// Status values.
const (
	StatusInstalled   Status = "installed"
	StatusUninstalled Status = "uninstalled"
	StatusFailed      Status = "failed"
	// StatusSkipped is a folder the batch never reached, because an earlier
	// one failed or the run was canceled. Nothing was written there.
	StatusSkipped Status = "skipped"
	// StatusRolledBack is a folder canceled mid-way: what had been written
	// was taken back out.
	StatusRolledBack Status = "rolled back"
)

// ReportRow is one line of a finished apply's report.
type ReportRow struct {
	Status Status
	// Folder names the folder the row is about.
	Folder string
	// Detail is what happened, in a sentence.
	Detail string
	// Lines are the finer points (files unchanged, warnings), for a
	// "show details" toggle.
	Lines []string
}

// Report is the outcome of a whole apply.
type Report struct {
	Rows []ReportRow
	// Failed is true when any folder ended in an error.
	Failed bool
	// Canceled is true when the run was stopped before it finished.
	Canceled bool
}

// Summary is the report's one-line heading.
func (r Report) Summary() string {
	switch {
	case r.Canceled:
		return "Canceled before it finished"
	case r.Failed:
		return "Finished with a problem"
	default:
		return "All done"
	}
}

// NewReport turns what an apply did into rows a window can show.
func NewReport(outcomes []Outcome, canceled bool) Report {
	rep := Report{Canceled: canceled}
	for _, o := range outcomes {
		row := ReportRow{Folder: folderLabel(o.Op.Dir)}
		switch {
		case !o.Attempted:
			row.Status = StatusSkipped
			row.Detail = "Not started — nothing was changed here."
		case o.Err != nil && canceled:
			row.Status = StatusRolledBack
			row.Detail = "Canceled; anything already written was rolled back."
		case o.Err != nil:
			row.Status = StatusFailed
			row.Detail = FriendlyError(o.Err)
			row.Lines = []string{o.Err.Error()}
			rep.Failed = true
		case o.Op.Uninstall != nil:
			row.Status = StatusUninstalled
			row.Detail, row.Lines = describeUninstall(o)
		default:
			row.Status = StatusInstalled
			row.Detail, row.Lines = describeInstall(o)
		}
		rep.Rows = append(rep.Rows, row)
	}
	return rep
}

func describeInstall(o Outcome) (string, []string) {
	req, res := o.Op.Install, o.Result
	detail := fmt.Sprintf("ReShade %s (%s), %d file(s) written", req.Version, req.Flavor, len(res.Written))
	if res.Bytes > 0 {
		detail += " (" + HumanSize(res.Bytes) + ")"
	}
	var lines []string
	if n := len(res.Skipped); n > 0 {
		lines = append(lines, fmt.Sprintf("%d file(s) unchanged.", n))
	}
	if n := len(res.Removed); n > 0 {
		lines = append(lines, fmt.Sprintf("%d file(s) from a previous install removed.", n))
	}
	for _, w := range res.Warnings {
		lines = append(lines, "! "+w)
	}
	return detail, lines
}

func describeUninstall(o Outcome) (string, []string) {
	res := o.Removed
	detail := fmt.Sprintf("%d file(s) removed", len(res.Removed))
	var lines []string
	if n := len(res.Kept); n > 0 {
		lines = append(lines, fmt.Sprintf("%d file(s) kept: modified since installation.", n))
	}
	if n := len(res.Restored); n > 0 {
		lines = append(lines, fmt.Sprintf("%d backed-up file(s) restored.", n))
	}
	return detail, lines
}

// DescribeConflict says what is already in the way of an install and what
// will happen to it, in a short line.
func DescribeConflict(c install.Conflict, overwrite, backup bool) string {
	name := baseName(c.Path)
	switch {
	case c.Kind == install.ConflictConfig:
		return name + " — your settings, kept"
	case c.Kind == install.ConflictManaged:
		return name + " — yours, replaced"
	case overwrite && backup:
		return fmt.Sprintf("%s (%s) — replaced, original saved", name, HumanSize(c.Size))
	case overwrite:
		return fmt.Sprintf("%s (%s) — replaced, original discarded", name, HumanSize(c.Size))
	default:
		// The size often identifies the file: a 24 MiB dxgi.dll is some
		// other injector, not a stale ReShade.
		return fmt.Sprintf("%s (%s) — kept, so ReShade will not load", name, HumanSize(c.Size))
	}
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
