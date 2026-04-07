package policy

import (
	"fmt"
	"strings"

	"qorvexus/internal/config"
)

type Verdict string

const (
	VerdictAllow Verdict = "allow"
	VerdictDeny  Verdict = "deny"
)

type Result struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
	Risk    string  `json:"risk"`
}

type Engine struct {
	cfg config.ToolsConfig
}

func NewEngine(cfg config.ToolsConfig) *Engine {
	return &Engine{cfg: cfg}
}

func (e *Engine) EvaluateCommand(command string) Result {
	cmd := strings.TrimSpace(strings.ToLower(command))
	if cmd == "" {
		return Result{Verdict: VerdictDeny, Risk: "low", Reason: "empty command"}
	}
	for _, blocked := range e.cfg.BlockedCommands {
		if blocked != "" && strings.Contains(cmd, strings.ToLower(blocked)) {
			return Result{
				Verdict: VerdictDeny,
				Risk:    "critical",
				Reason:  fmt.Sprintf("command contains blocked pattern %q", blocked),
			}
		}
	}
	dangerous := []string{
		"rm -rf /", "mkfs", "shutdown", "reboot", "userdel", "dd if=", "git reset --hard", "git checkout --",
	}
	for _, pattern := range dangerous {
		if strings.Contains(cmd, pattern) {
			return Result{
				Verdict: VerdictDeny,
				Risk:    "critical",
				Reason:  fmt.Sprintf("command matches dangerous pattern %q", pattern),
			}
		}
	}
	return Result{
		Verdict: VerdictAllow,
		Risk:    classifyRisk(cmd),
		Reason:  "allowed by policy",
	}
}

func classifyRisk(command string) string {
	switch {
	case strings.Contains(command, "curl ") || strings.Contains(command, "wget ") || strings.Contains(command, "scp "):
		return "medium"
	case strings.Contains(command, "apt ") || strings.Contains(command, "npm install") || strings.Contains(command, "go install"):
		return "high"
	case strings.Contains(command, "mv ") || strings.Contains(command, "cp ") || strings.Contains(command, ">"):
		return "medium"
	default:
		return "low"
	}
}
