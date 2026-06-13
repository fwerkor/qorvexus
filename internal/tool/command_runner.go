package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/commandenv"
	"qorvexus/internal/config"
)

const (
	defaultToolCommandTimeoutSeconds = 60
	maxToolCommandTimeoutSeconds     = 3600
	defaultToolCommandOutputBytes    = 64 * 1024
	gracefulToolTerminationTimeout   = 5 * time.Second
	killToolTerminationTimeout       = 2 * time.Second
)

type managedSignal int

const (
	managedSignalTerminate managedSignal = iota
	managedSignalKill
)

type commandRunOptions struct {
	Dir            string
	Env            []string
	TimeoutSeconds int
	MaxOutputBytes int
}

type commandRunResult struct {
	Stdout           string `json:"stdout"`
	Stderr           string `json:"stderr"`
	ExitCode         int    `json:"exit_code"`
	TimedOut         bool   `json:"timed_out"`
	DurationMS       int64  `json:"duration_ms"`
	Truncated        bool   `json:"truncated"`
	TerminationError string `json:"termination_error,omitempty"`
}

func (r commandRunResult) CombinedOutput() string {
	out := strings.TrimSpace(r.Stdout)
	if serr := strings.TrimSpace(r.Stderr); serr != "" {
		if out != "" {
			out += "\n"
		}
		out += "[stderr]\n" + serr
	}
	if r.Truncated {
		if out != "" {
			out += "\n"
		}
		out += "[truncated output; showing tail]"
	}
	if strings.TrimSpace(r.TerminationError) != "" {
		if out != "" {
			out += "\n"
		}
		out += "[termination]\n" + strings.TrimSpace(r.TerminationError)
	}
	return out
}

func runShellCommand(ctx context.Context, cfg config.ToolsConfig, command string, opts commandRunOptions) (commandRunResult, error) {
	cmd, err := commandenv.ShellCommandContext(context.Background(), cfg.CommandShell, command)
	if err != nil {
		return commandRunResult{ExitCode: -1}, err
	}
	return runManagedCommand(ctx, cmd, opts)
}

func runExecutableCommand(ctx context.Context, name string, args []string, opts commandRunOptions) (commandRunResult, error) {
	cmd, err := commandenv.CommandContext(context.Background(), name, args...)
	if err != nil {
		return commandRunResult{ExitCode: -1}, err
	}
	return runManagedCommand(ctx, cmd, opts)
}

func runManagedCommand(ctx context.Context, cmd *exec.Cmd, opts commandRunOptions) (commandRunResult, error) {
	if strings.TrimSpace(opts.Dir) != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Env, opts.Env...)
	}
	timeout := clampToolCommandTimeout(opts.TimeoutSeconds)
	maxOutputBytes := clampToolCommandOutputLimit(opts.MaxOutputBytes)
	perStreamLimit := maxInt(1, maxOutputBytes/2)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return commandRunResult{ExitCode: -1}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return commandRunResult{ExitCode: -1}, err
	}

	stdoutTail := newCommandTailBuffer(perStreamLimit)
	stderrTail := newCommandTailBuffer(perStreamLimit)
	configureManagedCommand(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return commandRunResult{ExitCode: -1, DurationMS: int64(time.Since(start) / time.Millisecond)}, err
	}

	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		_, _ = io.Copy(stdoutTail, stdoutPipe)
	}()
	go func() {
		defer readers.Done()
		_, _ = io.Copy(stderrTail, stderrPipe)
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var waitErr error
	timedOut := false
	terminationError := ""
	select {
	case waitErr = <-waitDone:
	case <-runCtx.Done():
		timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		terminationError = terminateManagedCommand(cmd, waitDone)
		waitErr = runCtx.Err()
	}
	readers.Wait()

	result := commandRunResult{
		Stdout:           stdoutTail.String(),
		Stderr:           stderrTail.String(),
		ExitCode:         commandExitCode(cmd, waitErr),
		TimedOut:         timedOut,
		DurationMS:       int64(time.Since(start) / time.Millisecond),
		Truncated:        stdoutTail.Truncated() || stderrTail.Truncated(),
		TerminationError: terminationError,
	}
	if waitErr != nil {
		if timedOut {
			return result, fmt.Errorf("command timed out after %d seconds", timeout)
		}
		return result, waitErr
	}
	return result, nil
}

func clampToolCommandTimeout(timeoutSeconds int) int {
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultToolCommandTimeoutSeconds
	}
	return maxInt(1, minInt(timeoutSeconds, maxToolCommandTimeoutSeconds))
}

func clampToolCommandOutputLimit(maxOutputBytes int) int {
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultToolCommandOutputBytes
	}
	return maxInt(1, maxOutputBytes)
}

func commandExitCode(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

type commandTailBuffer struct {
	keepBytes  int
	data       []byte
	totalBytes int
}

func newCommandTailBuffer(keepBytes int) *commandTailBuffer {
	return &commandTailBuffer{keepBytes: maxInt(1, keepBytes), data: make([]byte, 0, keepBytes)}
}

func (b *commandTailBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.totalBytes += len(p)
	b.data = append(b.data, p...)
	if overflow := len(b.data) - b.keepBytes; overflow > 0 {
		b.data = b.data[overflow:]
	}
	return len(p), nil
}

func (b *commandTailBuffer) String() string {
	return string(bytes.ToValidUTF8(b.data, []byte("�")))
}

func (b *commandTailBuffer) Truncated() bool {
	return b.totalBytes > len(b.data)
}
