package skill

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name                   string
	Description            string
	Homepage               string `yaml:"homepage"`
	UserInvocable          *bool  `yaml:"user-invocable"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
	CommandDispatch        string `yaml:"command-dispatch"`
	CommandTool            string `yaml:"command-tool"`
	CommandArgMode         string `yaml:"command-arg-mode"`
	Metadata               string `yaml:"metadata"`
	Instructions           string
	Location               string
	Gating                 Gating
}

type Gating struct {
	OpenClaw struct {
		Always     bool     `json:"always"`
		Homepage   string   `json:"homepage"`
		OS         []string `json:"os"`
		PrimaryEnv string   `json:"primaryEnv"`
		Requires   struct {
			Bins    []string `json:"bins"`
			AnyBins []string `json:"anyBins"`
			Env     []string `json:"env"`
		} `json:"requires"`
	} `json:"openclaw"`
}

type Loader struct{}

func NewLoader() *Loader { return &Loader{} }

func (l *Loader) LoadDirs(dirs []string) ([]Skill, error) {
	var skills []Skill
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
			sk, err := l.loadFile(skillPath)
			if err != nil {
				continue
			}
			if sk.eligible() {
				skills = append(skills, sk)
			}
		}
	}
	return skills, nil
}

func (l *Loader) loadFile(path string) (Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, fmt.Errorf("skill %s missing frontmatter", path)
	}
	parts := strings.SplitN(text[4:], "\n---\n", 2)
	if len(parts) != 2 {
		return Skill{}, fmt.Errorf("skill %s malformed frontmatter", path)
	}
	sk := Skill{}
	if err := yaml.Unmarshal([]byte(parts[0]), &sk); err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	if sk.Name == "" || sk.Description == "" {
		return Skill{}, fmt.Errorf("skill %s missing name or description", path)
	}
	sk.Instructions = strings.TrimSpace(parts[1])
	sk.Location = filepath.Dir(path)
	if sk.Metadata != "" {
		if err := yaml.Unmarshal([]byte(sk.Metadata), &sk.Gating); err != nil {
			_ = err
		}
	}
	return sk, nil
}

func (s Skill) eligible() bool {
	if s.Gating.OpenClaw.Always {
		return true
	}
	if len(s.Gating.OpenClaw.OS) > 0 {
		found := false
		for _, osName := range s.Gating.OpenClaw.OS {
			if osName == runtime.GOOS {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, bin := range s.Gating.OpenClaw.Requires.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			return false
		}
	}
	if len(s.Gating.OpenClaw.Requires.AnyBins) > 0 {
		found := false
		for _, bin := range s.Gating.OpenClaw.Requires.AnyBins {
			if _, err := exec.LookPath(bin); err == nil {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func Prompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, skill := range skills {
		if skill.DisableModelInvocation {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s (location: %s)\n", skill.Name, skill.Description, skill.Location)
	}
	return strings.TrimSpace(b.String())
}
