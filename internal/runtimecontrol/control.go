package runtimecontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	EnvControlDir = "QORVEXUS_SUPERVISOR_CONTROL_DIR"
	EnvSourceRoot = "QORVEXUS_SOURCE_ROOT"
)

const (
	ActionRestart      = "restart"
	ActionSwitchBinary = "switch_binary"
)

type Request struct {
	Action      string    `json:"action"`
	BinaryPath  string    `json:"binary_path,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

type State struct {
	Mode           string    `json:"mode"`
	ControlDir     string    `json:"control_dir,omitempty"`
	ChildPID       int       `json:"child_pid,omitempty"`
	BinaryPath     string    `json:"binary_path,omitempty"`
	SourceRoot     string    `json:"source_root,omitempty"`
	ChildStartedAt time.Time `json:"child_started_at,omitempty"`
	LastRestartAt  time.Time `json:"last_restart_at,omitempty"`
	LastRequest    *Request  `json:"last_request,omitempty"`
}

type Client struct {
	controlDir string
}

func NewClient(controlDir string) *Client {
	return &Client{controlDir: strings.TrimSpace(controlDir)}
}

func NewClientFromEnv() *Client {
	return NewClient(os.Getenv(EnvControlDir))
}

func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.controlDir) != ""
}

func (c *Client) ControlDir() string {
	return c.controlDir
}

func (c *Client) RequestRestart(reason string) error {
	return c.Request(Request{
		Action: ActionRestart,
		Reason: reason,
	})
}

func (c *Client) RequestSwitchBinary(binaryPath string, reason string) error {
	return c.Request(Request{
		Action:     ActionSwitchBinary,
		BinaryPath: binaryPath,
		Reason:     reason,
	})
}

func (c *Client) Request(req Request) error {
	if !c.Enabled() {
		return errors.New("runtime is not supervised")
	}
	req.Action = strings.TrimSpace(req.Action)
	switch req.Action {
	case "", ActionRestart:
		req.Action = ActionRestart
	case ActionSwitchBinary:
		if strings.TrimSpace(req.BinaryPath) == "" {
			return errors.New("binary path is required for switch_binary")
		}
	default:
		return fmt.Errorf("unsupported runtime control action %q", req.Action)
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}
	return writeJSONAtomically(requestPath(c.controlDir), req)
}

func LoadPendingRequest(controlDir string) (Request, bool, error) {
	raw, err := os.ReadFile(requestPath(controlDir))
	if err != nil {
		if os.IsNotExist(err) {
			return Request{}, false, nil
		}
		return Request{}, false, err
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return Request{}, false, err
	}
	return req, true, nil
}

func ClearPendingRequest(controlDir string) error {
	err := os.Remove(requestPath(controlDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ReadState(controlDir string) (State, error) {
	raw, err := os.ReadFile(statePath(controlDir))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func WriteState(controlDir string, state State) error {
	if strings.TrimSpace(controlDir) == "" {
		return errors.New("control dir is required")
	}
	state.ControlDir = controlDir
	return writeJSONAtomically(statePath(controlDir), state)
}

func requestPath(controlDir string) string {
	return filepath.Join(controlDir, "request.json")
}

func statePath(controlDir string) string {
	return filepath.Join(controlDir, "state.json")
}

func writeJSONAtomically(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
