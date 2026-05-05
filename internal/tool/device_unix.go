//go:build !windows

package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func configureBackgroundProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func statDisk(path string) (systemDiskStat, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return systemDiskStat{}, err
	}
	total := fs.Blocks * uint64(fs.Bsize)
	free := fs.Bfree * uint64(fs.Bsize)
	avail := fs.Bavail * uint64(fs.Bsize)
	used := total - free
	usage := ""
	if total > 0 {
		usage = fmt.Sprintf("%.2f", float64(used)*100/float64(total))
	}
	return systemDiskStat{
		Path:       path,
		TotalBytes: total,
		FreeBytes:  free,
		AvailBytes: avail,
		UsedBytes:  used,
		UsagePct:   usage,
	}, nil
}

func parseSignal(value string) (os.Signal, error) {
	switch strings.TrimSpace(strings.ToUpper(value)) {
	case "", "TERM", "SIGTERM":
		return syscall.SIGTERM, nil
	case "KILL", "SIGKILL":
		return syscall.SIGKILL, nil
	case "INT", "SIGINT":
		return syscall.SIGINT, nil
	case "HUP", "SIGHUP":
		return syscall.SIGHUP, nil
	case "STOP", "SIGSTOP":
		return syscall.SIGSTOP, nil
	case "CONT", "SIGCONT":
		return syscall.SIGCONT, nil
	default:
		return nil, fmt.Errorf("unsupported signal %q", value)
	}
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGSTOP:
		return "SIGSTOP"
	case syscall.SIGCONT:
		return "SIGCONT"
	default:
		return fmt.Sprintf("SIGNAL(%v)", sig)
	}
}
