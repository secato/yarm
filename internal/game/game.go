// Package game models games and their executables: the things YARM can
// install ReShade into.
package game

// Game is a discovered or manually added game.
type Game struct {
	// ID is "steam:<appid>" or "manual:<sha1(root)[:12]>".
	ID string
	// Name is the display name.
	Name string
	// Provider is the name of the platform.Provider that discovered this
	// game ("steam", "manual").
	Provider string
	// Root is the absolute path to the game's install directory.
	Root string
}

// Arch is a PE image's target architecture.
type Arch string

// Arch values.
const (
	ArchUnknown Arch = "unknown"
	ArchX86     Arch = "x86"
	ArchX64     Arch = "x64"
)

// API is a guessed graphics API, based on which DLLs an executable imports.
type API string

// API values.
const (
	APIUnknown API = "unknown"
	APID3D8    API = "d3d8"
	APID3D9    API = "d3d9"
	APID3D10   API = "d3d10"
	APID3D11   API = "d3d11"
	APID3D12   API = "d3d12"
	APIDXGI    API = "dxgi"
	APIOpenGL  API = "opengl"
	APIVulkan  API = "vulkan"
)

// Supported reports whether YARM can install ReShade for this API.
// D3D8 and Vulkan are recognized but not supported yet.
func (a API) Supported() bool {
	switch a {
	case APID3D8, APIVulkan:
		return false
	default:
		return true
	}
}

// RecommendedDLL returns the ReShade DLL name to install for this API, or
// "" when there's no safe default and the user must choose explicitly
// (unknown or unsupported APIs).
func (a API) RecommendedDLL() string {
	switch a {
	case APID3D9:
		return "d3d9.dll"
	case APID3D10, APID3D11, APID3D12, APIDXGI:
		return "dxgi.dll"
	case APIOpenGL:
		return "opengl32.dll"
	default:
		return ""
	}
}

// Executable is a candidate .exe found under a game's root.
type Executable struct {
	// Path is relative to the game root, using OS-native separators.
	Path string
	// Skipped marks executables that matched a common non-game pattern
	// (uninstallers, redistributables, anti-cheat, ...). Still listed so a
	// "show all" toggle can reveal them.
	Skipped bool
	Arch    Arch
	API     API
}
