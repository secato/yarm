package artifacts

import (
	"runtime"
	"testing"
)

// Both branches must be assertable on any CI runner, so this test never
// consults runtime.GOOS to decide what to expect.
func TestNeedsD3DCompiler(t *testing.T) {
	tests := []struct {
		target TargetOS
		want   bool
		why    string
	}{
		{OSLinux, true, "Wine's builtin d3dcompiler_47 fails on some shaders"},
		{OSWindows, false, "Windows ships d3dcompiler_47.dll as a system component"},
		{OSDarwin, false, "not an install target in v1"},
		{TargetOS("plan9"), false, "unknown platforms default to not downloading"},
		{TargetOS(""), false, "an unset target must not trigger a 38 MB download"},
	}
	for _, tt := range tests {
		if got := NeedsD3DCompiler(tt.target); got != tt.want {
			t.Errorf("NeedsD3DCompiler(%q) = %v, want %v (%s)", tt.target, got, tt.want, tt.why)
		}
	}
}

func TestCurrentTargetOS(t *testing.T) {
	got := CurrentTargetOS()

	switch runtime.GOOS {
	case "windows":
		if got != OSWindows {
			t.Errorf("CurrentTargetOS() = %q on windows", got)
		}
	case "darwin":
		if got != OSDarwin {
			t.Errorf("CurrentTargetOS() = %q on darwin", got)
		}
	default:
		if got != OSLinux {
			t.Errorf("CurrentTargetOS() = %q on %s", got, runtime.GOOS)
		}
	}

	// Whatever platform this runs on, the value must be one the rules
	// above understand.
	if !NeedsD3DCompiler(got) && got == OSLinux {
		t.Error("inconsistent: linux should need d3dcompiler")
	}
}
