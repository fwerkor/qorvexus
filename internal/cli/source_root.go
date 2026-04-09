package cli

import (
	"os"
	"path/filepath"
	"strings"
)

func discoverSourceRoot(candidates ...string) string {
	for _, candidate := range candidates {
		root, ok := findSourceRoot(candidate)
		if ok {
			return root
		}
	}
	return ""
}

func findSourceRoot(start string) (string, bool) {
	if strings.TrimSpace(start) == "" {
		return "", false
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if pathExists(filepath.Join(abs, "go.mod")) && pathExists(filepath.Join(abs, "cmd", "qorvexus", "main.go")) {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return "", false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
