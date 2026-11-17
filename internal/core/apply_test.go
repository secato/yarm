package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/secato/yarm/internal/install"
)

func installOp(dir string) Op {
	return Op{Dir: dir, Install: &install.Request{Version: "6.8.0", Flavor: install.FlavorNormal}}
}

// recordingUninstaller notes what it was asked to remove.
type recordingUninstaller struct {
	calls []install.UninstallRequest
	err   error
}

func (r *recordingUninstaller) Uninstall(req install.UninstallRequest) (install.UninstallResult, error) {
	r.calls = append(r.calls, req)
	return install.UninstallResult{Removed: []string{"Game/dxgi.dll"}}, r.err
}

// A batch runs every op in order and narrates progress with the folder only
// when there is more than one to tell apart.
func TestRunOpsRunsInOrderAndNarrates(t *testing.T) {
	inst := &fakeInstaller{
		updates: []ProgressUpdate{{Label: "Downloading ReShade", Done: 1, Total: 2}},
		result:  install.Result{Written: []string{"a", "b"}, Bytes: 2048},
	}
	un := &recordingUninstaller{}
	ops := []Op{installOp("Ship"), {Dir: "Release", Uninstall: &install.UninstallRequest{GameID: "g", Exe: "Release/Game.exe"}}}

	var labels []string
	out := RunOps(context.Background(), inst, un, ops, func(u ProgressUpdate) { labels = append(labels, u.Label) })

	if len(out) != 2 || !out[0].Attempted || !out[1].Attempted {
		t.Fatalf("outcomes = %+v", out)
	}
	if len(un.calls) != 1 {
		t.Errorf("uninstaller called %d times", len(un.calls))
	}
	want := []string{"Ship/ Downloading ReShade", "Release/ Removing ReShade"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("labels = %v, want %v", labels, want)
	}

	// One op reads without the folder prefix.
	labels = nil
	RunOps(context.Background(), inst, un, ops[:1], func(u ProgressUpdate) { labels = append(labels, u.Label) })
	if len(labels) != 1 || labels[0] != "Downloading ReShade" {
		t.Errorf("single-op labels = %v", labels)
	}
}

// The batch stops at the first failure; the ones behind it are reported as
// never started.
func TestRunOpsStopsAtTheFirstFailure(t *testing.T) {
	inst := &fakeInstaller{err: errors.New("disk full")}
	ops := []Op{installOp("A"), installOp("B")}

	out := RunOps(context.Background(), inst, &recordingUninstaller{}, ops, func(ProgressUpdate) {})
	if !out[0].Attempted || out[0].Err == nil {
		t.Errorf("first outcome = %+v", out[0])
	}
	if out[1].Attempted {
		t.Error("the second op ran after the first failed")
	}

	rep := NewReport(out, false)
	if !rep.Failed || rep.Summary() != "Finished with a problem" {
		t.Errorf("report = %+v", rep)
	}
	if rep.Rows[0].Status != StatusFailed || rep.Rows[1].Status != StatusSkipped {
		t.Errorf("statuses = %v, %v", rep.Rows[0].Status, rep.Rows[1].Status)
	}
	if rep.Rows[0].Folder != "A/" || !strings.Contains(rep.Rows[0].Detail, "disk full") {
		t.Errorf("failed row = %+v", rep.Rows[0])
	}
}

// A canceled run reports the interrupted folder as rolled back and never
// starts the rest.
func TestRunOpsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inst := &fakeInstaller{blockUntilCanceled: true}
	ops := []Op{installOp("A"), installOp("B")}

	go cancel()
	out := RunOps(ctx, inst, &recordingUninstaller{}, ops, func(ProgressUpdate) {})
	rep := NewReport(out, true)

	if !rep.Canceled || rep.Summary() != "Canceled before it finished" {
		t.Errorf("report = %+v", rep)
	}
	for _, row := range rep.Rows {
		if row.Status != StatusRolledBack && row.Status != StatusSkipped {
			t.Errorf("row %+v: want rolled back or skipped", row)
		}
	}
}

func TestNewReportDescribesSuccess(t *testing.T) {
	out := []Outcome{
		{Op: installOp("A"), Attempted: true, Result: install.Result{
			Written: []string{"a"}, Bytes: 5 << 20, Skipped: []string{"x"}, Removed: []string{"y"}, Warnings: []string{"careful"},
		}},
		{Op: Op{Dir: "B", Uninstall: &install.UninstallRequest{}}, Attempted: true, Removed: install.UninstallResult{
			Removed: []string{"r"}, Kept: []string{"k"}, Restored: []string{"s"},
		}},
	}
	rep := NewReport(out, false)

	if rep.Failed || rep.Summary() != "All done" {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Rows[0].Status != StatusInstalled || !strings.Contains(rep.Rows[0].Detail, "5.0 MiB") {
		t.Errorf("install row = %+v", rep.Rows[0])
	}
	if len(rep.Rows[0].Lines) != 3 {
		t.Errorf("install lines = %v, want skipped, removed and the warning", rep.Rows[0].Lines)
	}
	if rep.Rows[1].Status != StatusUninstalled || len(rep.Rows[1].Lines) != 2 {
		t.Errorf("uninstall row = %+v", rep.Rows[1])
	}
}

func TestHumanSize(t *testing.T) {
	tests := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"}
	for n, want := range tests {
		if got := HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestOpVerbs(t *testing.T) {
	if o := installOp("A"); o.Verb() != "installed" || o.FailVerb() != "install" {
		t.Errorf("install op verbs = %q, %q", o.Verb(), o.FailVerb())
	}
	o := Op{Uninstall: &install.UninstallRequest{}}
	if o.Verb() != "uninstalled" || o.FailVerb() != "uninstall" {
		t.Errorf("uninstall op verbs = %q, %q", o.Verb(), o.FailVerb())
	}
}
