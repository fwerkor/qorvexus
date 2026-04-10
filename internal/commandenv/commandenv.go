package commandenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	"/run/current-system/sw/bin",
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
	ensureHome(envMap)
	envMap["PATH"] = augmentPath(envMap["PATH"], envMap["HOME"])
	ensureGoEnv(envMap)
	if !contains(order, "PATH") {
		order = append(order, "PATH")
	}
	for _, key := range []string{"HOME", "GOPATH", "GOMODCACHE"} {
		if _, ok := envMap[key]; ok && !contains(order, key) {
			order = append(order, key)
		}
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
	return augmentPath("", "")
}

func augmentPath(existing string, home string) string {
	parts := filepath.SplitList(existing)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts)+len(defaultPathEntries)+8)
	candidates := append(parts, additionalPathEntries(home)...)
	candidates = append(candidates, defaultPathEntries...)
	for _, part := range candidates {
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

func additionalPathEntries(home string) []string {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	entries := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
	}
	entries = append(entries, nvmBinEntries(home)...)
	return existingDirs(entries)
}

func nvmBinEntries(home string) []string {
	pattern := filepath.Join(home, ".nvm", "versions", "node", "*", "bin")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	currentBin := filepath.Join(home, ".nvm", "versions", "node", "current", "bin")
	if pathExists(currentBin) {
		matches = append([]string{currentBin}, matches...)
	}
	return matches
}

func existingDirs(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if pathExists(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func ensureHome(envMap map[string]string) {
	if strings.TrimSpace(envMap["HOME"]) != "" {
		return
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		envMap["HOME"] = home
	}
}

func ensureGoEnv(envMap map[string]string) {
	home := strings.TrimSpace(envMap["HOME"])
	if home == "" {
		return
	}
	if strings.TrimSpace(envMap["GOPATH"]) == "" {
		envMap["GOPATH"] = filepath.Join(home, "go")
	}
	if strings.TrimSpace(envMap["GOMODCACHE"]) == "" {
		envMap["GOMODCACHE"] = filepath.Join(envMap["GOPATH"], "pkg", "mod")
	}
}

func isExecutable(mode os.FileMode) bool {
	return mode&0o111 != 0
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
