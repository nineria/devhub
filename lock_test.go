package main

import (
	"path/filepath"
	"testing"
)

func TestLockInstanceIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ostler.lock")
	first, err := lockInstance(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockInstance(path); err == nil {
		second.Close()
		t.Fatal("a second instance got the lock")
	}
	first.Close()
	again, err := lockInstance(path)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	again.Close()
}
