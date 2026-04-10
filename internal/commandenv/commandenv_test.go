package commandenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAugmentedEnvAddsCommonSystemPaths(t *testing.T) {
	env := AugmentedEnv([]string{"HOME=/tmp/qorvexus"})
	pathValue := pathValueFromEnv(env)
	for _, want := range []string{"/usr/bin", "/bin"} {
		if !strings.Contains(pathValue, want) {
			t.Fatalf("expected PATH to contain %s, got %q", want, pathValue)
		}
	}
}

func TestResolveExecutableUsesProvidedPath(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(binDir, "hello")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveExecutable("hello", []string{"PATH=" + binDir})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != scriptPath {
		t.Fatalf("expected %s, got %s", scriptPath, resolved)
	}
}

func TestAugmentedEnvAddsDefaultGoEnv(t *testing.T) {
	env := AugmentedEnv([]string{"HOME=/tmp/qorvexus-home"})
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if got := values["GOPATH"]; got != "/tmp/qorvexus-home/go" {
		t.Fatalf("expected GOPATH default, got %q", got)
	}
	if got := values["GOMODCACHE"]; got != "/tmp/qorvexus-home/go/pkg/mod" {
		t.Fatalf("expected GOMODCACHE default, got %q", got)
	}
}
