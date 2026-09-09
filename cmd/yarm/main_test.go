package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

// yarm takes no commands. Someone typing one is reaching for a
// subcommand that used to exist, so the error has to say that rather
// than leaving them to guess at the syntax.
func TestAnArgumentIsNotACommand(t *testing.T) {
	_, err := parseOptions([]string{"games"}, io.Discard)
	if err == nil {
		t.Fatal("want an error for a leftover argument")
	}
	for _, want := range []string{"games", "no commands"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestFlagsSetTheirOptions(t *testing.T) {
	// Cleared so a developer's own environment cannot turn these on.
	t.Setenv("YARM_DEBUG", "")
	t.Setenv("NO_COLOR", "")

	tests := []struct {
		name string
		args []string
		want options
	}{
		{"no arguments", nil, options{}},
		{"debug", []string{"-debug"}, options{debug: true}},
		{"no color", []string{"--no-color"}, options{noColor: true}},
		{"version", []string{"-version"}, options{showVersion: true}},
		{"together", []string{"-debug", "-no-color"}, options{debug: true, noColor: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args, io.Discard)
			if err != nil {
				t.Fatalf("parseOptions(%q): %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("parseOptions(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

// Both switches have an environment fallback, which is how they are
// usually set: NO_COLOR by convention, YARM_DEBUG when someone is asked
// for a log on a bug report.
func TestTheEnvironmentCanSetThemToo(t *testing.T) {
	t.Setenv("YARM_DEBUG", "1")
	t.Setenv("NO_COLOR", "true")

	got, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if !got.debug || !got.noColor {
		t.Errorf("parseOptions with the environment set = %+v, want both on", got)
	}
}

// A flag always wins over an absent variable, and neither is allowed to
// turn the other off.
func TestAnUnsetVariableDoesNotUndoAFlag(t *testing.T) {
	t.Setenv("YARM_DEBUG", "0")
	t.Setenv("NO_COLOR", "")

	got, err := parseOptions([]string{"-debug"}, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if !got.debug {
		t.Error("-debug was undone by YARM_DEBUG=0")
	}
}

func TestHelpIsNotAFailure(t *testing.T) {
	var out strings.Builder
	_, err := parseOptions([]string{"-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseOptions(-h) error = %v, want flag.ErrHelp", err)
	}
	// run() turns ErrHelp into a clean exit, so the usage text is the
	// whole output and has to actually say how to start the program.
	if !strings.Contains(out.String(), "no arguments") {
		t.Errorf("usage does not say how to open yarm:\n%s", out.String())
	}
}

func TestAnUnknownFlagIsRejected(t *testing.T) {
	if _, err := parseOptions([]string{"-nope"}, io.Discard); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}
