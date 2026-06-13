//go:build windows

package tool

import (
	"os"
	"os/exec"
	"time"
)

func configureManagedCommand(cmd *exec.Cmd) {}

func signalManagedCommand(cmd *exec.Cmd, sig managedSignal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if sig == managedSignalKill {
		return cmd.Process.Kill()
	}
	return cmd.Process.Signal(os.Interrupt)
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
		return "process did not exit after kill"
	}
}
