//go:build !windows

package tool

import (
	"os/exec"
	"syscall"
	"time"
)

func configureManagedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalManagedCommand(cmd *exec.Cmd, sig managedSignal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		signal := syscall.SIGTERM
		if sig == managedSignalKill {
			signal = syscall.SIGKILL
		}
		return syscall.Kill(-pgid, signal)
	}
	if sig == managedSignalKill {
		return cmd.Process.Kill()
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

func terminateManagedCommand(cmd *exec.Cmd, waitDone <-chan error) string {
	_ = signalManagedCommand(cmd, managedSignalTerminate)
	select {
	case <-waitDone:
		return ""
	case <-time.After(gracefulToolTerminationTimeout):
	}
	_ = signalManagedCommand(cmd, managedSignalKill)
	select {
	case <-waitDone:
		return ""
	case <-time.After(killToolTerminationTimeout):
		return "process did not exit after SIGKILL"
	}
}
