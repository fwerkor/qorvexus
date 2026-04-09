package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSourceRootFindsRepositoryLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainDir := filepath.Join(root, "cmd", "qorvexus")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got := discoverSourceRoot(nested)
	if got != root {
		t.Fatalf("expected %s, got %s", root, got)
	}
}

func TestDiscoverSourceRootReturnsEmptyWhenMissing(t *testing.T) {
	if got := discoverSourceRoot(t.TempDir()); got != "" {
		t.Fatalf("expected empty result, got %s", got)
	}
}
