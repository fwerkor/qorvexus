package policy

import (
	"fmt"
	"strings"
	"unicode"

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
		if blocked != "" && commandMatchesPattern(cmd, strings.ToLower(blocked)) {
			return Result{
				Verdict: VerdictDeny,
				Risk:    "critical",
				Reason:  fmt.Sprintf("command contains blocked pattern %q", blocked),
			}
		}
	}
	if deletesRoot(cmd) {
		return Result{
			Verdict: VerdictDeny,
			Risk:    "critical",
			Reason:  `command attempts recursive forced deletion of "/"`,
		}
	}
	dangerous := []string{
		"mkfs", "shutdown", "reboot", "userdel", "dd if=", "git reset --hard", "git checkout --", "poweroff",
	}
	for _, pattern := range dangerous {
		if commandMatchesPattern(cmd, pattern) {
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

func commandMatchesPattern(command string, pattern string) bool {
	command = strings.TrimSpace(strings.ToLower(command))
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	if command == "" || pattern == "" {
		return false
	}
	if pattern == "rm -rf /" {
		return deletesRoot(command)
	}
	return strings.Contains(command, pattern)
}

func deletesRoot(command string) bool {
	commands := splitShellCommand(command)
	for _, words := range commands {
		if len(words) == 0 {
			continue
		}
		for {
			before := len(words)
			for len(words) > 0 && isEnvAssignment(words[0]) {
				words = words[1:]
			}
			for len(words) > 0 && isCommandPrefix(words[0]) {
				words = words[1:]
			}
			if len(words) == before {
				break
			}
		}
		if len(words) == 0 || words[0] != "rm" {
			continue
		}
		recursive := false
		for _, arg := range words[1:] {
			if strings.HasPrefix(arg, "-") && arg != "-" {
				if strings.Contains(arg, "r") || strings.Contains(arg, "R") {
					recursive = true
				}
				continue
			}
			if recursive && isRootPath(arg) {
				return true
			}
		}
	}
	return false
}

func splitShellCommand(command string) [][]string {
	var out [][]string
	var current []string
	var b strings.Builder
	var quote rune
	escaped := false
	flushWord := func() {
		if b.Len() == 0 {
			return
		}
		current = append(current, strings.ToLower(b.String()))
		b.Reset()
	}
	flushCommand := func() {
		flushWord()
		if len(current) > 0 {
			out = append(out, current)
			current = nil
		}
	}
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flushWord()
		case r == ';' || r == '&' || r == '|':
			flushCommand()
		default:
			b.WriteRune(r)
		}
	}
	flushCommand()
	return out
}

func isEnvAssignment(word string) bool {
	if strings.HasPrefix(word, "-") {
		return false
	}
	idx := strings.Index(word, "=")
	return idx > 0 && idx < len(word)-1
}

func isCommandPrefix(word string) bool {
	switch word {
	case "sudo", "doas", "env", "command", "builtin", "time", "nice", "nohup":
		return true
	default:
		return false
	}
}

func isRootPath(arg string) bool {
	arg = strings.TrimSpace(arg)
	for strings.HasSuffix(arg, "/") && len(arg) > 1 {
		arg = strings.TrimSuffix(arg, "/")
	}
	return arg == "/"
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
