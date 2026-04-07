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
	}, nil)

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

func TestBrowserWorkflowToolPassesActionsAndMode(t *testing.T) {
	tempDir := t.TempDir()
	runnerPath := filepath.Join(tempDir, "runner.sh")
	runner := `#!/bin/sh
echo "{"
echo "  \"mode\": \"$QORVEXUS_PLAYWRIGHT_MODE\","
echo "  \"profile\": \"$QORVEXUS_PLAYWRIGHT_PROFILE_NAME\","
echo "  \"state\": \"$QORVEXUS_PLAYWRIGHT_STORAGE_STATE_NAME\","
echo "  \"browser\": \"$QORVEXUS_PLAYWRIGHT_BROWSER\","
echo "  \"actions\":"
cat "$QORVEXUS_PLAYWRIGHT_ACTIONS_FILE"
echo "}"
`
	if err := os.WriteFile(runnerPath, []byte(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	headless := true
	tool := NewBrowserWorkflowTool(config.ToolsConfig{
		CommandShell:             "bash",
		PlaywrightCommand:        fmt.Sprintf("%q", runnerPath),
		PlaywrightBrowser:        "chromium",
		PlaywrightProfileDir:     filepath.Join(tempDir, "profiles"),
		PlaywrightStateDir:       filepath.Join(tempDir, "state"),
		PlaywrightArtifactsDir:   filepath.Join(tempDir, "artifacts"),
		PlaywrightTimeoutSeconds: 30,
		PlaywrightHeadless:       &headless,
		MaxCommandBytes:          16 * 1024,
	}, nil)

	out, err := invokeTool(t, tool, context.Background(), map[string]any{
		"start_url":     "https://example.com",
		"profile":       "Research Main",
		"storage_state": "Research State",
		"retry_count":   2,
		"actions": []map[string]any{
			{"type": "goto", "url": "https://example.com/login"},
			{"type": "wait_for", "selector": "#login"},
			{"type": "screenshot", "path": "page.png"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	output, ok := payload["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured output map, got %#v", payload["output"])
	}
	if got := output["mode"]; got != "actions" {
		t.Fatalf("expected actions mode, got %#v", got)
	}
	if got := output["profile"]; got != "research-main" {
		t.Fatalf("expected sanitized profile, got %#v", got)
	}
	actions, ok := output["actions"].(map[string]any)
	if !ok {
		t.Fatalf("expected action payload map, got %#v", output["actions"])
	}
	items, ok := actions["actions"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("expected 3 actions, got %#v", actions["actions"])
	}
}

func TestBrowserWorkflowToolPreservesAdvancedActionPayloads(t *testing.T) {
	tempDir := t.TempDir()
	runnerPath := filepath.Join(tempDir, "runner.sh")
	runner := `#!/bin/sh
cat "$QORVEXUS_PLAYWRIGHT_ACTIONS_FILE"
`
	if err := os.WriteFile(runnerPath, []byte(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	headless := true
	tool := NewBrowserWorkflowTool(config.ToolsConfig{
		CommandShell:             "bash",
		PlaywrightCommand:        fmt.Sprintf("%q", runnerPath),
		PlaywrightBrowser:        "chromium",
		PlaywrightProfileDir:     filepath.Join(tempDir, "profiles"),
		PlaywrightStateDir:       filepath.Join(tempDir, "state"),
		PlaywrightArtifactsDir:   filepath.Join(tempDir, "artifacts"),
		PlaywrightTimeoutSeconds: 30,
		PlaywrightHeadless:       &headless,
		MaxCommandBytes:          16 * 1024,
	}, nil)

	out, err := invokeTool(t, tool, context.Background(), map[string]any{
		"profile":       "Assistant Main",
		"storage_state": "Assistant State",
		"retry_count":   3,
		"actions": []map[string]any{
			{"type": "open_tab", "url": "https://example.com/docs"},
			{"type": "fill_form", "fields": map[string]any{"Email": "owner@example.com", "Remember me": true}, "submit_text": "Continue"},
			{"type": "upload_files", "selector": "input[type=file]", "files": []any{"./fixtures/resume.pdf"}},
			{"type": "paginate_extract", "item_selector": ".item", "fields": map[string]any{"title": ".title", "href": "a@href"}, "next_selector": ".next", "max_pages": 4},
			{"type": "check_login_state", "logged_in_selector": "[data-auth=ready]", "logged_out_text": "Sign in", "require_authenticated": false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	output, ok := payload["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected decoded output map, got %#v", payload["output"])
	}
	actions, ok := output["actions"].([]any)
	if !ok || len(actions) != 5 {
		t.Fatalf("expected 5 actions, got %#v", output["actions"])
	}
	fillForm, ok := actions[1].(map[string]any)
	if !ok {
		t.Fatalf("expected fill_form action map, got %#v", actions[1])
	}
	fields, ok := fillForm["fields"].(map[string]any)
	if !ok || fields["Email"] != "owner@example.com" {
		t.Fatalf("expected nested fill_form fields, got %#v", fillForm["fields"])
	}
	paginate, ok := actions[3].(map[string]any)
	if !ok || paginate["max_pages"] != float64(4) {
		t.Fatalf("expected paginate_extract max_pages, got %#v", actions[3])
	}
}

func TestPlaywrightManagerAutoInstallsRuntime(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(binDir, "node")
	npmPath := filepath.Join(binDir, "npm")
	nodeScript := `#!/bin/sh
if [ "$2" = "install" ]; then
  exit 0
fi
exit 0
`
	npmScript := `#!/bin/sh
mkdir -p "$PWD/node_modules/playwright"
echo '#!/usr/bin/env node' > "$PWD/node_modules/playwright/cli.js"
exit 0
`
	if err := os.WriteFile(nodePath, []byte(nodeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(npmPath, []byte(npmScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	autoInstall := true
	manager := NewPlaywrightManager(config.ToolsConfig{
		PlaywrightRuntimeDir:     filepath.Join(tempDir, "runtime"),
		PlaywrightBrowser:        "chromium",
		PlaywrightInstallBrowser: []string{"chromium"},
		PlaywrightAutoInstall:    &autoInstall,
	})

	status, err := manager.EnsureInstalled(context.Background(), "chromium")
	if err != nil {
		t.Fatal(err)
	}
	if !status.ModuleReady || !status.BrowserReady {
		t.Fatalf("expected runtime and browser ready, got %#v", status)
	}
	if !pathExists(filepath.Join(status.RuntimeDir, "node_modules", "playwright", "cli.js")) {
		t.Fatalf("expected installed playwright cli in %s", status.RuntimeDir)
	}
	if !pathExists(filepath.Join(status.RuntimeDir, ".chromium.ready")) {
		t.Fatalf("expected browser ready stamp in %s", status.RuntimeDir)
	}
}
