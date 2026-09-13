//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// Register inherited stdin with Go's poller so Close can interrupt a blocked Read.
func openStdin() (*os.File, error) {
	fd, err := syscall.Dup(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("failed to duplicate stdin: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("failed to configure stdin: %w", err)
	}
	return os.NewFile(uintptr(fd), "stdin"), nil
}
