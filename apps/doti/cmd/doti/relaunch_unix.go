//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// relaunch replaces this process with the binary that just replaced it on disk.
//
// execve rather than spawn-and-exit: the same pid keeps the same terminal, the
// shell's job control stays intact, and there is no window where a parent is
// waiting on a child that has taken its screen. The reader pressed r after a
// self-update; what they should get is the new version running where the old
// one was, not a second process behind it.
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding this binary to relaunch it: %w", err)
	}
	// Only reached on failure: on success this process no longer exists.
	if err := execSelf(exe, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("relaunching %s: %w", exe, err)
	}
	return nil
}

// execSelf is syscall.Exec, indirected so a test can assert what would be
// exec'd. Calling the real one from a test would replace the test.
var execSelf = syscall.Exec
