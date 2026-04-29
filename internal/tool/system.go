package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"qorvexus/internal/commandenv"
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
		Description: "Run a short-lived command on the local system and return stdout/stderr. Use this for brief synchronous shell work when you need an immediate result. Do not use it for long-running or stateful jobs such as apt update, package installs, servers, watchers, or builds that may outlive a short timeout; use manage_process with action=start for those.",
		Parameters: schemaObject(map[string]any{
			"command":         schemaString("Shell command to run. Prefer a focused one-shot command whose result can be captured immediately."),
			"timeout_seconds": schemaInteger("Optional timeout in seconds. Defaults to 60. Increase only for slightly longer synchronous work, not for background jobs."),
		}, "command"),
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

	cmd, err := commandenv.ShellCommandContext(cmdCtx, t.cfg.CommandShell, input.Command)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
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
		Description: "Fetch a web page or API over HTTP when browser automation is unnecessary. Prefer this for simple GET or API calls. If the task needs login state, JavaScript execution, clicking, uploads, or multi-step navigation, use browser_workflow or playwright instead.",
		Parameters: schemaObject(map[string]any{
			"url":    schemaString("Absolute URL to request."),
			"method": schemaString("HTTP method such as GET or POST. Defaults to GET when omitted."),
			"body":   schemaString("Optional request body as raw text. Most useful with POST, PUT, or PATCH."),
		}, "url"),
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
	text := sanitizeHTTPBodyForModel(buf.String(), resp.Header.Get("Content-Type"), t.cfg.MaxCommandBytes)
	if len(text) > t.cfg.MaxCommandBytes {
		text = text[:t.cfg.MaxCommandBytes] + "\n[truncated]"
	}
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, text), nil
}

var (
	htmlScriptStylePattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>|<svg\b[^>]*>.*?</svg>`)
	htmlTagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlWhitespacePattern  = regexp.MustCompile(`\s+`)
	htmlAnchorPattern      = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
)

type httpHTMLSummary struct {
	Text             string     `json:"text,omitempty"`
	Links            []httpLink `json:"links,omitempty"`
	OmittedHTMLChars int        `json:"omitted_html_chars,omitempty"`
}

type httpLink struct {
	Text string `json:"text,omitempty"`
	Href string `json:"href"`
}

func sanitizeHTTPBodyForModel(body string, contentType string, limit int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	var decoded any
	if json.Unmarshal([]byte(body), &decoded) == nil {
		cleaned := sanitizeJSONHTMLFields(decoded)
		raw, err := json.MarshalIndent(cleaned, "", "  ")
		if err == nil {
			return string(raw)
		}
	}
	if looksLikeHTML(body, contentType) {
		summary := summarizeHTML(body, limit)
		raw, err := json.MarshalIndent(summary, "", "  ")
		if err == nil {
			return string(raw)
		}
	}
	return body
}

func sanitizeJSONHTMLFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if text, ok := item.(string); ok && shouldSummarizeHTMLField(key, text) {
				out[key] = summarizeHTML(text, 4096)
				continue
			}
			out[key] = sanitizeJSONHTMLFields(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeJSONHTMLFields(item)
		}
		return out
	default:
		return value
	}
}

func shouldSummarizeHTMLField(key string, value string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "html" || key == "body" || key == "markup" || key == "svg" {
		return looksLikeHTML(value, "")
	}
	return len(value) > 2000 && looksLikeHTML(value, "")
}

func looksLikeHTML(value string, contentType string) bool {
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "text/html") {
		return true
	}
	value = strings.ToLower(value)
	return strings.Contains(value, "<html") || strings.Contains(value, "<div") || strings.Contains(value, "<a ") || strings.Contains(value, "<svg")
}

func summarizeHTML(value string, limit int) httpHTMLSummary {
	cleaned := htmlScriptStylePattern.ReplaceAllString(value, " ")
	links := extractHTMLLinks(cleaned, 40)
	text := htmlTagPattern.ReplaceAllString(cleaned, " ")
	text = html.UnescapeString(htmlWhitespacePattern.ReplaceAllString(text, " "))
	text = strings.TrimSpace(text)
	textLimit := 3000
	if limit > 0 && limit < textLimit {
		textLimit = limit
	}
	if len([]rune(text)) > textLimit {
		runes := []rune(text)
		text = string(runes[:textLimit]) + "..."
	}
	return httpHTMLSummary{
		Text:             text,
		Links:            links,
		OmittedHTMLChars: len(value),
	}
}

func extractHTMLLinks(value string, maxLinks int) []httpLink {
	matches := htmlAnchorPattern.FindAllStringSubmatch(value, maxLinks)
	links := make([]httpLink, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		text := htmlTagPattern.ReplaceAllString(match[2], " ")
		text = html.UnescapeString(htmlWhitespacePattern.ReplaceAllString(text, " "))
		links = append(links, httpLink{
			Text: strings.TrimSpace(text),
			Href: html.UnescapeString(strings.TrimSpace(match[1])),
		})
	}
	return links
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
		Description: "Run a custom Playwright script only when browser_workflow is not expressive enough. The script already receives playwright, chromium/firefox/webkit, browserType, browser, context, page, and qorvexus; do not launch another browser unless necessary. Defaults fill browser=chromium, headless, profile, storage, and timeout. Prefer page.goto(url, {waitUntil: 'domcontentloaded'}) plus explicit selectors over networkidle.",
		Parameters: schemaObject(map[string]any{
			"script":             schemaString("JavaScript Playwright script body to execute. Return a string/object. Use existing page/context; helper qorvexus.artifactPath(name, ext) builds artifact paths."),
			"profile":            schemaString("Optional persistent browser profile name so cookies and login state can be reused across runs."),
			"storage_state":      schemaString("Optional named storage-state snapshot to load before the run."),
			"persist_profile":    schemaBoolean("Whether to keep browser profile changes after the run. Useful when login state should survive."),
			"save_storage_state": schemaBoolean("Whether to save updated storage state after the run for later reuse."),
			"browser":            schemaString("Optional browser engine override. Defaults to chromium."),
			"headless":           schemaBoolean("Whether to run headless. Defaults follow runtime config."),
			"timeout_seconds":    schemaInteger("Optional overall timeout in seconds for the automation run."),
		}, "script"),
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
