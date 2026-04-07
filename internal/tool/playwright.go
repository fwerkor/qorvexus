package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

type PlaywrightManager struct {
	cfg config.ToolsConfig

	mu          sync.Mutex
	readyModule bool
	readyBrows  map[string]bool
}

type PlaywrightBootstrapStatus struct {
	RuntimeDir        string   `json:"runtime_dir"`
	NodePath          string   `json:"node_path,omitempty"`
	NPMPath           string   `json:"npm_path,omitempty"`
	Browser           string   `json:"browser"`
	InstalledBrowsers []string `json:"installed_browsers,omitempty"`
	AutoInstall       bool     `json:"auto_install"`
	ModuleReady       bool     `json:"module_ready"`
	BrowserReady      bool     `json:"browser_ready"`
}

func NewPlaywrightManager(cfg config.ToolsConfig) *PlaywrightManager {
	return &PlaywrightManager{
		cfg:        cfg,
		readyBrows: map[string]bool{},
	}
}

func (m *PlaywrightManager) EnsureInstalled(ctx context.Context, browser string) (PlaywrightBootstrapStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(browser) == "" {
		browser = m.cfg.PlaywrightBrowser
	}
	if strings.TrimSpace(browser) == "" {
		browser = "chromium"
	}
	autoInstall := true
	if m.cfg.PlaywrightAutoInstall != nil {
		autoInstall = *m.cfg.PlaywrightAutoInstall
	}
	status := PlaywrightBootstrapStatus{
		RuntimeDir:  m.cfg.PlaywrightRuntimeDir,
		Browser:     browser,
		AutoInstall: autoInstall,
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return status, fmt.Errorf("node is required for Playwright runtime: %w", err)
	}
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return status, fmt.Errorf("npm is required for Playwright runtime: %w", err)
	}
	status.NodePath = nodePath
	status.NPMPath = npmPath

	if err := os.MkdirAll(m.cfg.PlaywrightRuntimeDir, 0o755); err != nil {
		return status, err
	}
	if err := writePlaywrightRuntimePackage(m.cfg.PlaywrightRuntimeDir); err != nil {
		return status, err
	}

	moduleDir := filepath.Join(m.cfg.PlaywrightRuntimeDir, "node_modules", "playwright")
	if !m.readyModule && !pathExists(moduleDir) {
		if !autoInstall {
			return status, fmt.Errorf("Playwright runtime is not installed and auto-install is disabled")
		}
		if _, err := runCommandInDir(ctx, m.cfg.PlaywrightRuntimeDir, nil, npmPath, "install", "--no-fund", "--no-audit", "playwright"); err != nil {
			return status, fmt.Errorf("install Playwright package: %w", err)
		}
	}
	if pathExists(moduleDir) {
		m.readyModule = true
		status.ModuleReady = true
	}

	browsers := normalizeInstallBrowsers(m.cfg, browser)
	for _, name := range browsers {
		readyPath := filepath.Join(m.cfg.PlaywrightRuntimeDir, "."+name+".ready")
		if m.readyBrows[name] || pathExists(readyPath) {
			m.readyBrows[name] = true
			continue
		}
		if !autoInstall {
			return status, fmt.Errorf("Playwright browser %q is not installed and auto-install is disabled", name)
		}
		cliPath := filepath.Join(m.cfg.PlaywrightRuntimeDir, "node_modules", "playwright", "cli.js")
		if !pathExists(cliPath) {
			return status, fmt.Errorf("Playwright CLI not found after install in %s", cliPath)
		}
		if _, err := runCommandInDir(ctx, m.cfg.PlaywrightRuntimeDir, nil, nodePath, cliPath, "install", name); err != nil {
			return status, fmt.Errorf("install Playwright browser %q: %w", name, err)
		}
		if err := os.WriteFile(readyPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
			return status, err
		}
		m.readyBrows[name] = true
	}
	status.BrowserReady = m.readyBrows[browser]
	status.InstalledBrowsers = installedBrowserList(m.readyBrows)
	return status, nil
}

type BrowserWorkflowTool struct {
	cfg     config.ToolsConfig
	manager *PlaywrightManager
}

func NewBrowserWorkflowTool(cfg config.ToolsConfig, manager *PlaywrightManager) *BrowserWorkflowTool {
	return &BrowserWorkflowTool{cfg: cfg, manager: manager}
}

func (t *BrowserWorkflowTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "browser_workflow",
		Description: "Run a structured browser workflow with retries, persistent login state, screenshots, PDFs, download indexing, and table extraction.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"start_url":          map[string]any{"type": "string"},
				"profile":            map[string]any{"type": "string"},
				"storage_state":      map[string]any{"type": "string"},
				"browser":            map[string]any{"type": "string"},
				"headless":           map[string]any{"type": "boolean"},
				"persist_profile":    map[string]any{"type": "boolean"},
				"save_storage_state": map[string]any{"type": "boolean"},
				"timeout_seconds":    map[string]any{"type": "integer"},
				"retry_count":        map[string]any{"type": "integer"},
				"actions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
			},
			"required": []string{"actions"},
		},
	}
}

func (t *BrowserWorkflowTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		StartURL         string           `json:"start_url"`
		Profile          string           `json:"profile"`
		StorageState     string           `json:"storage_state"`
		Browser          string           `json:"browser"`
		Headless         *bool            `json:"headless"`
		PersistProfile   *bool            `json:"persist_profile"`
		SaveStorageState *bool            `json:"save_storage_state"`
		TimeoutSeconds   int              `json:"timeout_seconds"`
		RetryCount       int              `json:"retry_count"`
		Actions          []map[string]any `json:"actions"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Actions) == 0 {
		return "", fmt.Errorf("browser workflow requires at least one action")
	}
	actionPayload, err := json.Marshal(map[string]any{
		"start_url":   input.StartURL,
		"retry_count": input.RetryCount,
		"actions":     input.Actions,
	})
	if err != nil {
		return "", err
	}
	request := playwrightExecutionRequest{
		Mode:             "actions",
		Payload:          actionPayload,
		Profile:          input.Profile,
		StorageState:     input.StorageState,
		Browser:          input.Browser,
		Headless:         input.Headless,
		PersistProfile:   input.PersistProfile,
		SaveStorageState: input.SaveStorageState,
		TimeoutSeconds:   input.TimeoutSeconds,
	}
	return runPlaywrightExecution(ctx, t.cfg, t.manager, request)
}

type playwrightExecutionRequest struct {
	Mode             string
	Payload          []byte
	Profile          string
	StorageState     string
	Browser          string
	Headless         *bool
	PersistProfile   *bool
	SaveStorageState *bool
	TimeoutSeconds   int
}

func runPlaywrightExecution(ctx context.Context, cfg config.ToolsConfig, manager *PlaywrightManager, req playwrightExecutionRequest) (string, error) {
	if strings.TrimSpace(cfg.PlaywrightCommand) == "" {
		return "", fmt.Errorf("playwright command is not configured")
	}
	if len(req.Payload) == 0 {
		return "", fmt.Errorf("playwright payload cannot be empty")
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = cfg.PlaywrightTimeoutSeconds
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 120
	}
	profileName := sanitizePlaywrightName(req.Profile)
	if profileName == "" {
		profileName = "default"
	}
	storageName := sanitizePlaywrightName(req.StorageState)
	if storageName == "" {
		storageName = profileName
	}
	persistProfile := true
	if req.PersistProfile != nil {
		persistProfile = *req.PersistProfile
	}
	saveStorageState := true
	if req.SaveStorageState != nil {
		saveStorageState = *req.SaveStorageState
	}
	headless := true
	if cfg.PlaywrightHeadless != nil {
		headless = *cfg.PlaywrightHeadless
	}
	if req.Headless != nil {
		headless = *req.Headless
	}
	browser := strings.TrimSpace(req.Browser)
	if browser == "" {
		browser = cfg.PlaywrightBrowser
	}
	if browser == "" {
		browser = "chromium"
	}
	var runtimeStatus PlaywrightBootstrapStatus
	var err error
	if manager != nil {
		runtimeStatus, err = manager.EnsureInstalled(ctx, browser)
		if err != nil {
			return "", err
		}
	}

	profileDir := filepath.Join(cfg.PlaywrightProfileDir, profileName)
	statePath := filepath.Join(cfg.PlaywrightStateDir, storageName+".json")
	artifactsDir := filepath.Join(cfg.PlaywrightArtifactsDir, profileName)
	for _, dir := range []string{filepath.Dir(statePath), artifactsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if persistProfile {
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return "", err
		}
	}
	payloadSuffix := ".txt"
	if req.Mode == "actions" {
		payloadSuffix = ".json"
	}
	payloadFile, err := os.CreateTemp("", "qorvexus-playwright-*"+payloadSuffix)
	if err != nil {
		return "", err
	}
	payloadPath := payloadFile.Name()
	if _, err := payloadFile.Write(req.Payload); err != nil {
		payloadFile.Close()
		_ = os.Remove(payloadPath)
		return "", err
	}
	if err := payloadFile.Close(); err != nil {
		_ = os.Remove(payloadPath)
		return "", err
	}
	defer os.Remove(payloadPath)

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, cfg.CommandShell, "-lc", cfg.PlaywrightCommand)
	cmd.Env = append(os.Environ(),
		"QORVEXUS_PLAYWRIGHT_MODE="+req.Mode,
		"QORVEXUS_PLAYWRIGHT_SCRIPT_FILE="+payloadPath,
		"QORVEXUS_PLAYWRIGHT_ACTIONS_FILE="+payloadPath,
		"QORVEXUS_PLAYWRIGHT_PROFILE_NAME="+profileName,
		"QORVEXUS_PLAYWRIGHT_PROFILE_DIR="+profileDir,
		"QORVEXUS_PLAYWRIGHT_STORAGE_STATE_NAME="+storageName,
		"QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE="+statePath,
		"QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE="+strconv.FormatBool(persistProfile),
		"QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE="+strconv.FormatBool(saveStorageState),
		"QORVEXUS_PLAYWRIGHT_BROWSER="+browser,
		"QORVEXUS_PLAYWRIGHT_HEADLESS="+strconv.FormatBool(headless),
		"QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS="+strconv.Itoa(req.TimeoutSeconds),
		"QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR="+artifactsDir,
		"QORVEXUS_PLAYWRIGHT_RUNTIME_DIR="+runtimeStatus.RuntimeDir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if serr := strings.TrimSpace(stderr.String()); serr != "" {
		if out != "" {
			out += "\n"
		}
		out += "[stderr]\n" + serr
	}
	if len(out) > cfg.MaxCommandBytes {
		out = out[:cfg.MaxCommandBytes] + "\n[truncated]"
	}
	if err != nil {
		return out, fmt.Errorf("playwright failed: %w", err)
	}
	result := map[string]any{
		"mode":               req.Mode,
		"profile":            profileName,
		"profile_dir":        profileDir,
		"storage_state":      storageName,
		"storage_state_path": statePath,
		"persist_profile":    persistProfile,
		"save_storage_state": saveStorageState,
		"browser":            browser,
		"headless":           headless,
		"timeout_seconds":    req.TimeoutSeconds,
		"artifacts_dir":      artifactsDir,
		"runtime":            runtimeStatus,
	}
	if decoded, ok := decodePlaywrightOutput(out); ok {
		result["output"] = decoded
	} else {
		result["output"] = out
	}
	rawResult, _ := json.MarshalIndent(result, "", "  ")
	return string(rawResult), nil
}

func normalizeInstallBrowsers(cfg config.ToolsConfig, requested string) []string {
	selected := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		selected = append(selected, value)
	}
	add(requested)
	for _, value := range cfg.PlaywrightInstallBrowser {
		add(value)
	}
	if len(selected) == 0 {
		selected = append(selected, "chromium")
	}
	return selected
}

func installedBrowserList(items map[string]bool) []string {
	out := make([]string, 0, len(items))
	for browser, ready := range items {
		if ready {
			out = append(out, browser)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sortStrings(out)
	return out
}

func writePlaywrightRuntimePackage(runtimeDir string) error {
	packagePath := filepath.Join(runtimeDir, "package.json")
	content := `{
  "name": "qorvexus-playwright-runtime",
  "private": true,
  "dependencies": {
    "playwright": "latest"
  }
}
`
	if existing, err := os.ReadFile(packagePath); err == nil && string(existing) == content {
		return nil
	}
	return os.WriteFile(packagePath, []byte(content), 0o644)
}

func runCommandInDir(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
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
	if err != nil {
		return out, err
	}
	return out, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func decodePlaywrightOutput(value string) (any, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
