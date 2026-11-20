package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockInstance takes an exclusive lock on path for as long as the returned file stays open. The kernel
// drops it when the process exits, even after a crash, so it can never go stale.
func lockInstance(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Ostler is already running (lock %s); stop it first, or let launchd manage it", path)
	}
	return f, nil
}
