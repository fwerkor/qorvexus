package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/policy"
	"qorvexus/internal/types"
)

type CommandTool struct {
	cfg    config.ToolsConfig
	policy *policy.Engine
}

func NewCommandTool(cfg config.ToolsConfig, engine *policy.Engine) *CommandTool {
	return &CommandTool{cfg: cfg, policy: engine}
}

func (t *CommandTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "run_command",
		Description: "Run a command on the local system. Use only when direct system interaction is necessary.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string"},
				"timeout_seconds": map[string]any{"type": "integer"},
			},
			"required": []string{"command"},
		},
	}
}

func (t *CommandTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	if !t.cfg.AllowCommandExecution {
		return "", fmt.Errorf("command execution is disabled")
	}
	var input struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 60
	}
	policyCtx := policyContextFromTool(ctx)
	var policyResult policy.Result
	if t.policy != nil {
		policyResult = t.policy.EvaluateCommandForContext(input.Command, policyCtx)
		if policyResult.Verdict != policy.VerdictAllow {
			return "", fmt.Errorf("command denied by policy: %s (risk=%s)", policyResult.Reason, policyResult.Risk)
		}
	}
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, t.cfg.CommandShell, "-lc", input.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if serr := strings.TrimSpace(stderr.String()); serr != "" {
		if out != "" {
			out += "\n"
		}
		out += "[stderr]\n" + serr
	}
	if len(out) > t.cfg.MaxCommandBytes {
		out = out[:t.cfg.MaxCommandBytes] + "\n[truncated]"
	}
	if err != nil {
		return out, fmt.Errorf("command failed: %w", err)
	}
	if t.policy != nil {
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("[policy]\nrisk=%s\nreason=%s", policyResult.Risk, policyResult.Reason)
	}
	return out, nil
}

type HTTPTool struct {
	cfg config.ToolsConfig
}

func NewHTTPTool(cfg config.ToolsConfig) *HTTPTool { return &HTTPTool{cfg: cfg} }

func (t *HTTPTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "http_request",
		Description: "Fetch web pages or APIs when browsing is needed without a full browser.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":    map[string]any{"type": "string"},
				"method": map[string]any{"type": "string"},
				"body":   map[string]any{"type": "string"},
			},
			"required": []string{"url"},
		},
	}
}

func (t *HTTPTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		URL    string `json:"url"`
		Method string `json:"method"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if input.Method == "" {
		input.Method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, strings.NewReader(input.Body))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", t.cfg.HTTPUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	text := buf.String()
	if len(text) > t.cfg.MaxCommandBytes {
		text = text[:t.cfg.MaxCommandBytes] + "\n[truncated]"
	}
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, text), nil
}

type PlaywrightTool struct {
	cfg config.ToolsConfig
}

func NewPlaywrightTool(cfg config.ToolsConfig) *PlaywrightTool { return &PlaywrightTool{cfg: cfg} }

func (t *PlaywrightTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "playwright",
		Description: "Use a browser automation command to interact with websites through Playwright, with persistent browser profiles and storage state support.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"script":             map[string]any{"type": "string"},
				"profile":            map[string]any{"type": "string"},
				"storage_state":      map[string]any{"type": "string"},
				"persist_profile":    map[string]any{"type": "boolean"},
				"save_storage_state": map[string]any{"type": "boolean"},
				"browser":            map[string]any{"type": "string"},
				"headless":           map[string]any{"type": "boolean"},
				"timeout_seconds":    map[string]any{"type": "integer"},
			},
			"required": []string{"script"},
		},
	}
}

func (t *PlaywrightTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.cfg.PlaywrightCommand == "" {
		return "", fmt.Errorf("playwright command is not configured")
	}
	var input struct {
		Script           string `json:"script"`
		Profile          string `json:"profile"`
		StorageState     string `json:"storage_state"`
		PersistProfile   *bool  `json:"persist_profile"`
		SaveStorageState *bool  `json:"save_storage_state"`
		Browser          string `json:"browser"`
		Headless         *bool  `json:"headless"`
		TimeoutSeconds   int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Script) == "" {
		return "", fmt.Errorf("script cannot be empty")
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = t.cfg.PlaywrightTimeoutSeconds
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 120
	}
	profileName := sanitizePlaywrightName(input.Profile)
	if profileName == "" {
		profileName = "default"
	}
	storageName := sanitizePlaywrightName(input.StorageState)
	if storageName == "" {
		storageName = profileName
	}
	persistProfile := true
	if input.PersistProfile != nil {
		persistProfile = *input.PersistProfile
	}
	saveStorageState := true
	if input.SaveStorageState != nil {
		saveStorageState = *input.SaveStorageState
	}
	headless := true
	if t.cfg.PlaywrightHeadless != nil {
		headless = *t.cfg.PlaywrightHeadless
	}
	if input.Headless != nil {
		headless = *input.Headless
	}
	browser := strings.TrimSpace(input.Browser)
	if browser == "" {
		browser = t.cfg.PlaywrightBrowser
	}
	if browser == "" {
		browser = "chromium"
	}
	profileDir := filepath.Join(t.cfg.PlaywrightProfileDir, profileName)
	statePath := filepath.Join(t.cfg.PlaywrightStateDir, storageName+".json")
	artifactsDir := filepath.Join(t.cfg.PlaywrightArtifactsDir, profileName)
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
	scriptFile, err := os.CreateTemp("", "qorvexus-playwright-*.js")
	if err != nil {
		return "", err
	}
	scriptPath := scriptFile.Name()
	if _, err := scriptFile.WriteString(input.Script); err != nil {
		scriptFile.Close()
		_ = os.Remove(scriptPath)
		return "", err
	}
	if err := scriptFile.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return "", err
	}
	defer os.Remove(scriptPath)
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, t.cfg.CommandShell, "-lc", t.cfg.PlaywrightCommand)
	cmd.Env = append(os.Environ(),
		"QORVEXUS_PLAYWRIGHT_SCRIPT_FILE="+scriptPath,
		"QORVEXUS_PLAYWRIGHT_PROFILE_NAME="+profileName,
		"QORVEXUS_PLAYWRIGHT_PROFILE_DIR="+profileDir,
		"QORVEXUS_PLAYWRIGHT_STORAGE_STATE_NAME="+storageName,
		"QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE="+statePath,
		"QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE="+strconv.FormatBool(persistProfile),
		"QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE="+strconv.FormatBool(saveStorageState),
		"QORVEXUS_PLAYWRIGHT_BROWSER="+browser,
		"QORVEXUS_PLAYWRIGHT_HEADLESS="+strconv.FormatBool(headless),
		"QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS="+strconv.Itoa(input.TimeoutSeconds),
		"QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR="+artifactsDir,
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
	if len(out) > t.cfg.MaxCommandBytes {
		out = out[:t.cfg.MaxCommandBytes] + "\n[truncated]"
	}
	if err != nil {
		return out, fmt.Errorf("playwright failed: %w", err)
	}
	result := map[string]any{
		"profile":            profileName,
		"profile_dir":        profileDir,
		"storage_state":      storageName,
		"storage_state_path": statePath,
		"persist_profile":    persistProfile,
		"save_storage_state": saveStorageState,
		"browser":            browser,
		"headless":           headless,
		"timeout_seconds":    input.TimeoutSeconds,
		"artifacts_dir":      artifactsDir,
		"output":             out,
	}
	rawResult, _ := json.MarshalIndent(result, "", "  ")
	return string(rawResult), nil
}

func sanitizePlaywrightName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
