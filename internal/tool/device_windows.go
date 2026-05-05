//go:build windows

package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func configureBackgroundProcess(cmd *exec.Cmd) {}

func statDisk(path string) (systemDiskStat, error) {
	if path == "" {
		path = "."
	}
	absPath, err := filepath.Abs(path)
	if err == nil {
		path = absPath
	}

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return systemDiskStat{}, err
	}

	var avail, total, free uint64
	ret, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return systemDiskStat{}, callErr
		}
		return systemDiskStat{}, fmt.Errorf("GetDiskFreeSpaceExW failed")
	}

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
	case "", "TERM", "SIGTERM", "KILL", "SIGKILL":
		return os.Kill, nil
	case "INT", "SIGINT":
		return os.Interrupt, nil
	default:
		return nil, fmt.Errorf("unsupported signal %q on windows", value)
	}
}

func signalName(sig os.Signal) string {
	switch sig {
	case os.Kill:
		return "SIGKILL"
	case os.Interrupt:
		return "SIGINT"
	default:
		return fmt.Sprintf("SIGNAL(%v)", sig)
	}
}
