package policy

import (
	"fmt"
	"strings"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
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
	return e.EvaluateCommandForContext(command, types.ConversationContext{Trust: types.TrustOwner, IsOwner: true})
}

func (e *Engine) EvaluateCommandForContext(command string, ctx types.ConversationContext) Result {
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
		"rm -rf /", "mkfs", "shutdown", "reboot", "userdel", "dd if=", "git reset --hard", "git checkout --", "sudo ", "poweroff",
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
	if !ctx.IsOwner {
		risk := classifyRisk(cmd)
		if risk == "high" || strings.Contains(cmd, "git push") || strings.Contains(cmd, "ssh ") || strings.Contains(cmd, "systemctl ") || strings.Contains(cmd, "launchctl ") || strings.Contains(cmd, "kill ") {
			return Result{
				Verdict: VerdictDeny,
				Risk:    risk,
				Reason:  "non-owner context cannot execute elevated or outward-facing commands",
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
	case strings.Contains(command, "apt ") || strings.Contains(command, "npm install") || strings.Contains(command, "go install") || strings.Contains(command, "systemctl ") || strings.Contains(command, "launchctl ") || strings.Contains(command, "kill "):
		return "high"
	case strings.Contains(command, "mv ") || strings.Contains(command, "cp ") || strings.Contains(command, ">"):
		return "medium"
	default:
		return "low"
	}
}
