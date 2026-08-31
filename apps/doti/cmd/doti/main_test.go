package main

import (
	"os"
	"testing"
)

// The rule these two guard is small and was wrong in a way no test could see,
// because the wrong version was one expression inside build(). It is a
// function now so it can be asked directly.
func TestCanPromptNeedsBothStreams(t *testing.T) {
	// A pipe is what `curl ... | bash` hands to the process it execs, and
	// what a test can create. /dev/null is the CI shape.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devNull.Close() })

	regular, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { regular.Close() })

	for _, tc := range []struct {
		name          string
		stdout, stdin *os.File
	}{
		{"piped stdin, terminal stdout is the curl|bash case", regular, r},
		{"stdin from /dev/null is not a person", regular, devNull},
		{"both piped", w, r},
		{"a file for stdout", regular, regular},
		{"nil streams", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if canPrompt(tc.stdout, tc.stdin) {
				t.Error("canPrompt = true; nothing here can answer a question")
			}
		})
	}
}

// /dev/null is a character device, so the Stat().Mode()&os.ModeCharDevice
// test that shipped called it a terminal - which is how an unattended
// `doti install </dev/null` went looking for a vault password. Pinned
// separately from the table above because it is the specific regression.
func TestDevNullIsNotATerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	info, err := devNull.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("this platform does not make /dev/null a character device, so the old bug could not occur")
	}
	if isTerminal(devNull) {
		t.Error("isTerminal(/dev/null) = true; the ModeCharDevice heuristic is back")
	}
}
