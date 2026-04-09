package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/runtimecontrol"
)

const (
	supervisorPollInterval = 500 * time.Millisecond
	supervisorStopTimeout  = 10 * time.Second
)

type supervisedRuntime struct {
	configPath string
	binaryPath string
	sourceRoot string
	workingDir string
}

func runUnderSupervisor(ctx context.Context, configPath string) error {
	executablePath, err := os.Executable()
	if err != nil {
		return err
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return err
	}
	sourceRoot := discoverSourceRoot(
		os.Getenv(runtimecontrol.EnvSourceRoot),
		workingDir,
		filepath.Dir(configPath),
		filepath.Dir(executablePath),
	)
	rt := &supervisedRuntime{
		configPath: configPath,
		binaryPath: executablePath,
		sourceRoot: sourceRoot,
		workingDir: workingDir,
	}
	return rt.Run(ctx)
}

func (r *supervisedRuntime) Run(ctx context.Context) error {
	currentBinary := r.binaryPath
	var lastRequest *runtimecontrol.Request
	var lastRestartAt time.Time

outer:
	for {
		controlDir, err := supervisorControlDir(r.configPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(controlDir, 0o755); err != nil {
			return err
		}
		if err := runtimecontrol.ClearPendingRequest(controlDir); err != nil {
			return err
		}

		cmd, err := r.startChild(controlDir, currentBinary)
		if err != nil {
			return err
		}
		childStartedAt := time.Now().UTC()
		if err := runtimecontrol.WriteState(controlDir, runtimecontrol.State{
			Mode:           "supervised",
			ChildPID:       cmd.Process.Pid,
			BinaryPath:     currentBinary,
			SourceRoot:     r.sourceRoot,
			ChildStartedAt: childStartedAt,
			LastRestartAt:  lastRestartAt,
			LastRequest:    lastRequest,
		}); err != nil {
			return err
		}

		exitCh := make(chan error, 1)
		go func() {
			exitCh <- cmd.Wait()
		}()

		ticker := time.NewTicker(supervisorPollInterval)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				_ = stopChild(cmd, exitCh, supervisorStopTimeout)
				return nil
			case err := <-exitCh:
				ticker.Stop()
				if err != nil {
					return fmt.Errorf("qorvexus daemon exited: %w", err)
				}
				return nil
			case <-ticker.C:
				req, ok, err := runtimecontrol.LoadPendingRequest(controlDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "qorvexus supervisor: ignoring invalid restart request: %v\n", err)
					_ = runtimecontrol.ClearPendingRequest(controlDir)
					continue
				}
				if !ok {
					continue
				}
				if err := runtimecontrol.ClearPendingRequest(controlDir); err != nil {
					fmt.Fprintf(os.Stderr, "qorvexus supervisor: failed to clear restart request: %v\n", err)
					continue
				}
				nextBinary := currentBinary
				if req.Action == runtimecontrol.ActionSwitchBinary {
					nextBinary, err = normalizeBinaryPath(req.BinaryPath)
					if err != nil {
						fmt.Fprintf(os.Stderr, "qorvexus supervisor: ignoring invalid binary path %q: %v\n", req.BinaryPath, err)
						continue
					}
				}
				if err := stopChild(cmd, exitCh, supervisorStopTimeout); err != nil {
					ticker.Stop()
					return err
				}
				ticker.Stop()
				currentBinary = nextBinary
				reqCopy := req
				lastRequest = &reqCopy
				lastRestartAt = time.Now().UTC()
				continue outer
			}
		}
	}
}

func (r *supervisedRuntime) startChild(controlDir string, binaryPath string) (*exec.Cmd, error) {
	cmd := exec.Command(binaryPath, "daemon", "--config", r.configPath)
	cmd.Dir = r.workingDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s", runtimecontrol.EnvControlDir, controlDir))
	if strings.TrimSpace(r.sourceRoot) != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", runtimecontrol.EnvSourceRoot, r.sourceRoot))
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopChild(cmd *exec.Cmd, exitCh <-chan error, timeout time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-exitCh:
		return nil
	case <-timer.C:
		if err := cmd.Process.Kill(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "finished") {
			return err
		}
		<-exitCh
		return nil
	}
}

func supervisorControlDir(configPath string) (string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.DataDir, "supervisor"), nil
}

func normalizeBinaryPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("binary path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("binary path %s is a directory", abs)
	}
	return abs, nil
}
