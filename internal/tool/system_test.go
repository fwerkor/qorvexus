package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/config"
)

func TestPlaywrightToolPassesPersistentContextSettings(t *testing.T) {
	tempDir := t.TempDir()
	runnerPath := filepath.Join(tempDir, "runner.sh")
	runner := `#!/bin/sh
echo "PROFILE_DIR=$QORVEXUS_PLAYWRIGHT_PROFILE_DIR"
echo "STATE_FILE=$QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE"
echo "ARTIFACTS_DIR=$QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR"
echo "BROWSER=$QORVEXUS_PLAYWRIGHT_BROWSER"
echo "HEADLESS=$QORVEXUS_PLAYWRIGHT_HEADLESS"
echo "PERSIST=$QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE"
echo "SAVE_STATE=$QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE"
echo "TIMEOUT=$QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS"
echo "SCRIPT<<EOF"
cat "$QORVEXUS_PLAYWRIGHT_SCRIPT_FILE"
echo "EOF"
`
	if err := os.WriteFile(runnerPath, []byte(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	headless := true
	tool := NewPlaywrightTool(config.ToolsConfig{
		CommandShell:             "bash",
		PlaywrightCommand:        fmt.Sprintf("%q", runnerPath),
		PlaywrightBrowser:        "chromium",
		PlaywrightProfileDir:     filepath.Join(tempDir, "profiles"),
		PlaywrightStateDir:       filepath.Join(tempDir, "state"),
		PlaywrightArtifactsDir:   filepath.Join(tempDir, "artifacts"),
		PlaywrightTimeoutSeconds: 45,
		PlaywrightHeadless:       &headless,
		MaxCommandBytes:          16 * 1024,
	})

	out, err := invokeTool(t, tool, context.Background(), map[string]any{
		"script":             "return { ok: true, message: 'hello' };",
		"profile":            "Owner Main",
		"storage_state":      "Owner Login",
		"headless":           false,
		"persist_profile":    true,
		"save_storage_state": true,
		"timeout_seconds":    90,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["profile"]; got != "owner-main" {
		t.Fatalf("expected sanitized profile name, got %#v", got)
	}
	if got := payload["storage_state"]; got != "owner-login" {
		t.Fatalf("expected sanitized storage name, got %#v", got)
	}
	if got := payload["headless"]; got != false {
		t.Fatalf("expected overridden headless false, got %#v", got)
	}
	output, _ := payload["output"].(string)
	if !strings.Contains(output, "PROFILE_DIR=") || !strings.Contains(output, "owner-main") {
		t.Fatalf("expected profile dir in output, got %s", output)
	}
	if !strings.Contains(output, "STATE_FILE=") || !strings.Contains(output, "owner-login.json") {
		t.Fatalf("expected state file in output, got %s", output)
	}
	if !strings.Contains(output, "HEADLESS=false") {
		t.Fatalf("expected headless override in output, got %s", output)
	}
	if !strings.Contains(output, "return { ok: true, message: 'hello' };") {
		t.Fatalf("expected script file content in output, got %s", output)
	}
}
