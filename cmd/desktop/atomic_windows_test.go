//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWriteWaitsForWindowsReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := atomicWrite(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	done := make(chan error, 1)
	go func() { done <- atomicWrite(path, []byte("new")) }()
	select {
	case err := <-done:
		t.Fatalf("replacement should wait for the open Windows handle: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatal(string(data), err)
	}
}
