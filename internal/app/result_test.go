package app

import (
	"strings"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
)

// resultRequest is the little of a Request the result screen reads.
func resultRequest() install.Request {
	return install.Request{
		Version: "6.8.0",
		Flavor:  install.FlavorNormal,
		Exe:     game.Executable{Path: "Game/emberhollow.exe"},
	}
}

// singleInstallOutcome wraps one successful install.Result the way
// ProgressScreen would after running a single op.
func singleInstallOutcome(res install.Result) []folderOutcome {
	req := resultRequest()
	return []folderOutcome{{Op: folderOp{Install: &req}, Attempted: true, Result: res}}
}

// The result screen is the only place an install's footprint on disk is
// ever stated, so the size has to survive from the plan to the screen.
func TestInstallResultReportsHowMuchWasWritten(t *testing.T) {
	res := install.Result{
		Written: []string{"Game/dxgi.dll", "Game/ReShade.ini"},
		Bytes:   3 * 1024 * 1024,
	}
	body := NewApplyResultScreen(singleInstallOutcome(res), false).View(wizardEnv())

	if !strings.Contains(body, "2 file(s) written") {
		t.Errorf("result does not report the file count:\n%s", body)
	}
	if !strings.Contains(body, "3.0 MiB") {
		t.Errorf("result does not report the size:\n%s", body)
	}
}

// A run that wrote nothing must not claim a size.
func TestInstallResultOmitsAZeroSize(t *testing.T) {
	body := NewApplyResultScreen(singleInstallOutcome(install.Result{}), false).View(wizardEnv())
	if strings.Contains(body, "(0 B)") {
		t.Errorf("result states a zero size:\n%s", body)
	}
}
