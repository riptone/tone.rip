package main

import (
	"fmt"
	"os"
	"os/exec"
)

// relaunch starts the replacement binary and lets this process end.
//
// Windows has no execve, so the same pid cannot become the new program. The
// child inherits this console, and the parent returns immediately rather than
// waiting - two processes both reading one console's input is a race whoever
// wins, and the reader only ever sees one of them.
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding this binary to relaunch it: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunching %s: %w", exe, err)
	}
	return nil
}
