package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
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
	cfg     config.ToolsConfig
	manager *PlaywrightManager
}

func NewPlaywrightTool(cfg config.ToolsConfig, manager *PlaywrightManager) *PlaywrightTool {
	return &PlaywrightTool{cfg: cfg, manager: manager}
}

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
	return runPlaywrightExecution(ctx, t.cfg, t.manager, playwrightExecutionRequest{
		Mode:             "script",
		Payload:          []byte(input.Script),
		Profile:          input.Profile,
		StorageState:     input.StorageState,
		Browser:          input.Browser,
		Headless:         input.Headless,
		PersistProfile:   input.PersistProfile,
		SaveStorageState: input.SaveStorageState,
		TimeoutSeconds:   input.TimeoutSeconds,
	})
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
