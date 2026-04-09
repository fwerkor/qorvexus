package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"qorvexus/internal/runtimecontrol"
)

const selfApplyTimeout = 15 * time.Minute

func (a *appRuntime) runtimeApplyEnabled() bool {
	return a.cfg.Self.Enabled && (a.cfg.Self.AllowRuntimeApply == nil || *a.cfg.Self.AllowRuntimeApply)
}

func (a *appRuntime) runtimeMode() string {
	if a.runtimeControl != nil && a.runtimeControl.Enabled() {
		return "supervised"
	}
	return "standalone"
}

func (a *appRuntime) RequestRuntimeRestart(ctx context.Context, reason string) (string, error) {
	if !a.runtimeApplyEnabled() {
		return "", fmt.Errorf("runtime apply is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("runtime restarts require owner context")
	}
	if a.runtimeControl == nil || !a.runtimeControl.Enabled() {
		return "", fmt.Errorf("runtime is not supervised; start Qorvexus with `qorvexus start`")
	}
	if err := a.runtimeControl.RequestRestart(reason); err != nil {
		return "", err
	}
	a.logAudit(ctx, "request_runtime_restart", "ok", a.executablePath, map[string]any{"reason": reason})
	return "runtime restart requested; supervisor will relaunch the daemon", nil
}

func (a *appRuntime) ApplySelfUpdate(ctx context.Context, runTests bool, reason string) (string, error) {
	if !a.runtimeApplyEnabled() {
		return "", fmt.Errorf("runtime apply is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("self-update requires owner context")
	}
	if a.runtimeControl == nil || !a.runtimeControl.Enabled() {
		return "", fmt.Errorf("runtime is not supervised; start Qorvexus with `qorvexus start`")
	}
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()

	sourceRoot, err := a.selfUpdateSourceRoot()
	if err != nil {
		return "", err
	}
	applyCtx, cancel := context.WithTimeout(ctx, selfApplyTimeout)
	defer cancel()

	notes := []string{fmt.Sprintf("source root: %s", sourceRoot)}
	if runTests {
		out, err := a.runLocalCommand(applyCtx, sourceRoot, "go", "test", "./...")
		if err != nil {
			return "", formatCommandError("go test ./...", out, err)
		}
		if strings.TrimSpace(out) != "" {
			notes = append(notes, "test output:\n"+out)
		}
		notes = append(notes, "preflight tests passed")
	}

	targetPath, buildOut, err := a.buildSelfBinary(applyCtx, sourceRoot)
	if err != nil {
		return "", formatCommandError("go build ./cmd/qorvexus", buildOut, err)
	}
	if err := a.runtimeControl.RequestSwitchBinary(targetPath, reason); err != nil {
		return "", err
	}
	a.logAudit(ctx, "apply_self_update", "ok", targetPath, map[string]any{
		"reason":      reason,
		"run_tests":   runTests,
		"source_root": sourceRoot,
	})

	result := []string{
		fmt.Sprintf("built new runtime binary: %s", targetPath),
		"supervisor restart requested",
	}
	result = append(result, notes...)
	if strings.TrimSpace(buildOut) != "" {
		result = append(result, "build output:\n"+buildOut)
	}
	return strings.Join(result, "\n"), nil
}

func (a *appRuntime) buildSelfBinary(ctx context.Context, sourceRoot string) (string, string, error) {
	targetDir := filepath.Join(a.cfg.DataDir, "bin")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", "", err
	}
	targetName := "qorvexus-" + time.Now().UTC().Format("20060102-150405")
	if runtime.GOOS == "windows" {
		targetName += ".exe"
	}
	targetPath := filepath.Join(targetDir, targetName)
	out, err := a.runLocalCommand(ctx, sourceRoot, "go", "build", "-trimpath", "-o", targetPath, "./cmd/qorvexus")
	if err != nil {
		return "", out, err
	}
	return targetPath, out, nil
}

func (a *appRuntime) selfUpdateSourceRoot() (string, error) {
	if strings.TrimSpace(a.sourceRoot) != "" {
		return a.sourceRoot, nil
	}
	workingDir, _ := os.Getwd()
	a.sourceRoot = discoverSourceRoot(
		os.Getenv(runtimecontrol.EnvSourceRoot),
		workingDir,
		filepath.Dir(a.configPath),
		filepath.Dir(a.executablePath),
	)
	if strings.TrimSpace(a.sourceRoot) == "" {
		return "", fmt.Errorf("Qorvexus source tree not found; set %s or start from the repository root", runtimecontrol.EnvSourceRoot)
	}
	return a.sourceRoot, nil
}

func (a *appRuntime) runLocalCommand(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if serr := strings.TrimSpace(stderr.String()); serr != "" {
		if out != "" {
			out += "\n"
		}
		out += "[stderr]\n" + serr
	}
	if limit := a.cfg.Tools.MaxCommandBytes; limit > 0 && len(out) > limit {
		out = out[:limit] + "\n[truncated]"
	}
	return out, err
}

func formatCommandError(label string, out string, err error) error {
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	return fmt.Errorf("%s failed: %w\n%s", label, err, out)
}
