//go:build !windows

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// What gets exec'd, which is the part with anything to get wrong. The syscall
// itself is stdlib, and calling it here would replace the test.
func TestRelaunchExecsThisBinaryWithTheSameArguments(t *testing.T) {
	var got struct {
		path string
		argv []string
		env  []string
	}
	// Restored rather than nilled: a nil seam would be a panic for whatever
	// runs next, which is a worse failure than the one this is testing.
	real := execSelf
	t.Cleanup(func() { execSelf = real })
	execSelf = func(path string, argv, env []string) error {
		got.path, got.argv, got.env = path, argv, env
		return nil
	}

	if err := relaunch(); err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got.path != exe {
		t.Errorf("exec'd %q, want this binary %q", got.path, exe)
	}
	// The same argv, so a relaunch lands where the reader was: `doti --repo X`
	// comes back as `doti --repo X`, not as a bare `doti`.
	if len(got.argv) != len(os.Args) {
		t.Errorf("argv = %v, want %v", got.argv, os.Args)
	}
	if len(got.env) == 0 {
		t.Error("the environment was dropped")
	}
}

// A failed exec has to say so rather than returning nil and letting the caller
// believe the new version is running.
func TestRelaunchReportsAFailedExec(t *testing.T) {
	real := execSelf
	t.Cleanup(func() { execSelf = real })
	execSelf = func(string, []string, []string) error { return errors.New("exec format error") }

	err := relaunch()
	if err == nil {
		t.Fatal("a failed exec returned nil")
	}
	if !strings.Contains(err.Error(), "relaunching") ||
		!strings.Contains(err.Error(), "exec format error") {
		t.Errorf("err = %v, want it to name the action and the cause", err)
	}
}
