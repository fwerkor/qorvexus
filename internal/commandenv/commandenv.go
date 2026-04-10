package commandenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var defaultPathEntries = []string{
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
	"/snap/bin",
	"/usr/local/go/bin",
}

func AugmentedEnv(base []string) []string {
	if len(base) == 0 {
		base = os.Environ()
	}
	envMap := map[string]string{}
	order := make([]string, 0, len(base)+1)
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, seen := envMap[key]; !seen {
			order = append(order, key)
		}
		envMap[key] = value
	}
	envMap["PATH"] = augmentPath(envMap["PATH"])
	if !contains(order, "PATH") {
		order = append(order, "PATH")
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		value, ok := envMap[key]
		if !ok {
			continue
		}
		out = append(out, key+"="+value)
	}
	return out
}

func ShellCommandContext(ctx context.Context, shell string, command string) (*exec.Cmd, error) {
	env := AugmentedEnv(nil)
	shellPath, err := ResolveExecutable(shell, env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, shellPath, "-lc", command)
	cmd.Env = env
	return cmd, nil
}

func CommandContext(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	env := AugmentedEnv(nil)
	binaryPath, err := ResolveExecutable(name, env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = env
	return cmd, nil
}

func ResolveExecutable(name string, env []string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("executable name is required")
	}
	if strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	pathValue := pathValueFromEnv(AugmentedEnv(env))
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && isExecutable(info.Mode()) {
			return candidate, nil
		}
		if runtime.GOOS == "windows" {
			for _, ext := range []string{".exe", ".bat", ".cmd"} {
				windowsCandidate := candidate + ext
				if info, err := os.Stat(windowsCandidate); err == nil && !info.IsDir() {
					return windowsCandidate, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%q not found in PATH=%s", name, pathValue)
}

func pathValueFromEnv(env []string) string {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			return value
		}
	}
	return augmentPath("")
}

func augmentPath(existing string) string {
	parts := filepath.SplitList(existing)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts)+len(defaultPathEntries))
	for _, part := range append(parts, defaultPathEntries...) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func isExecutable(mode os.FileMode) bool {
	return mode&0o111 != 0
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
