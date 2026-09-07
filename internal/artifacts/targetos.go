package artifacts

import "runtime"

// TargetOS is the operating system an install targets.
//
// It is a value passed around rather than a read of runtime.GOOS, because
// what matters is the OS the *game* will run under, and because keeping it
// explicit lets both branches of every OS-dependent rule be tested on any
// CI runner.
type TargetOS string

// TargetOS values.
const (
	OSLinux   TargetOS = "linux"
	OSWindows TargetOS = "windows"
	OSDarwin  TargetOS = "darwin"
)

// CurrentTargetOS maps the running platform to a TargetOS. This is the one
// place runtime.GOOS is consulted; everything downstream takes the value as
// a parameter so it can be exercised for either platform.
func CurrentTargetOS() TargetOS {
	switch runtime.GOOS {
	case "windows":
		return OSWindows
	case "darwin":
		return OSDarwin
	default:
		return OSLinux
	}
}

// NeedsD3DCompiler reports whether an install targeting target must also
// place d3dcompiler_47.dll next to the ReShade DLL.
//
// Only Linux does. ReShade needs Microsoft's D3D compiler for D3D9/10/11
// effects; Windows already ships d3dcompiler_47.dll as a system component,
// so copying one there would be redundant at best. Under Wine/Proton the
// builtin implementation fails on some shaders, which is why the real
// Microsoft DLL is downloaded and placed in the game directory.
//
// Anything else returns false: not downloading a 38 MB installer is the
// safe default for a platform whose install rules are not defined.
func NeedsD3DCompiler(target TargetOS) bool {
	return target == OSLinux
}
