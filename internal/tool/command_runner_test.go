package tool

import (
	"context"
	"strings"
	"testing"
	"time"

	"qorvexus/internal/config"
)

func TestManagedCommandTimesOutAndReturnsTailOutput(t *testing.T) {
	cfg := config.ToolsConfig{CommandShell: "bash", MaxCommandBytes: 4096}
	start := time.Now()
	result, err := runShellCommand(context.Background(), cfg, "printf 'ready-before-timeout\\n'; sleep 30", commandRunOptions{
		TimeoutSeconds: 1,
		MaxOutputBytes: 4096,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !result.TimedOut {
		t.Fatalf("expected timed_out=true, got %#v", result)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("timeout handling took too long: %s", elapsed)
	}
	if !strings.Contains(result.CombinedOutput(), "ready-before-timeout") {
		t.Fatalf("expected partial stdout to be preserved, got %q", result.CombinedOutput())
	}
}

func TestManagedCommandKeepsTailWithoutUnboundedBuffer(t *testing.T) {
	cfg := config.ToolsConfig{CommandShell: "bash", MaxCommandBytes: 512}
	result, err := runShellCommand(context.Background(), cfg, "for i in $(seq 1 200); do printf 'line-%03d padding-padding-padding\\n' \"$i\"; done", commandRunOptions{
		TimeoutSeconds: 5,
		MaxOutputBytes: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatalf("expected truncated output, got %#v", result)
	}
	combined := result.CombinedOutput()
	if !strings.Contains(combined, "line-200") {
		t.Fatalf("expected tail output to contain final line, got %q", combined)
	}
	if strings.Contains(combined, "line-001") {
		t.Fatalf("expected old output to be dropped from tail buffer, got %q", combined)
	}
	if len(combined) > 900 {
		t.Fatalf("combined output grew beyond expected bounded size: %d", len(combined))
	}
}

func TestManagedCommandPreservesStderrOnFailure(t *testing.T) {
	cfg := config.ToolsConfig{CommandShell: "bash", MaxCommandBytes: 4096}
	result, err := runShellCommand(context.Background(), cfg, "printf 'stdout-context\\n'; printf 'stderr-context\\n' >&2; exit 7", commandRunOptions{
		TimeoutSeconds: 5,
		MaxOutputBytes: 4096,
	})
	if err == nil {
		t.Fatal("expected non-zero exit error")
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.ExitCode)
	}
	combined := result.CombinedOutput()
	if !strings.Contains(combined, "stdout-context") || !strings.Contains(combined, "stderr-context") {
		t.Fatalf("expected stdout and stderr context to be preserved, got %q", combined)
	}
}
