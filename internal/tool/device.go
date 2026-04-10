package tool

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"qorvexus/internal/commandenv"
	"qorvexus/internal/config"
	"qorvexus/internal/policy"
	"qorvexus/internal/types"
)

type SystemSnapshotTool struct{}

type systemDiskStat struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	AvailBytes uint64 `json:"available_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	UsagePct   string `json:"usage_pct,omitempty"`
}

func NewSystemSnapshotTool() *SystemSnapshotTool { return &SystemSnapshotTool{} }

func (t *SystemSnapshotTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "system_snapshot",
		Description: "Collect a structured snapshot of the local device, including OS details, resource health, network interfaces, and optionally top processes. Prefer this when you need situational awareness before acting on the machine.",
		Parameters: schemaObject(map[string]any{
			"include_processes": schemaBoolean("Whether to include a lightweight list of top processes in the snapshot."),
			"process_limit":     schemaInteger("Maximum number of processes to include when include_processes is true."),
		}),
	}
}

func (t *SystemSnapshotTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		IncludeProcesses bool `json:"include_processes"`
		ProcessLimit     int  `json:"process_limit"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	if input.ProcessLimit <= 0 {
		input.ProcessLimit = 8
	}
	type snapshot struct {
		Timestamp         time.Time           `json:"timestamp"`
		Hostname          string              `json:"hostname,omitempty"`
		OS                string              `json:"os"`
		Arch              string              `json:"arch"`
		NumCPU            int                 `json:"num_cpu"`
		CurrentUser       string              `json:"current_user,omitempty"`
		WorkingDirectory  string              `json:"working_directory,omitempty"`
		HomeDirectory     string              `json:"home_directory,omitempty"`
		UptimeSeconds     float64             `json:"uptime_seconds,omitempty"`
		LoadAverage       map[string]float64  `json:"load_average,omitempty"`
		Memory            map[string]uint64   `json:"memory,omitempty"`
		NetworkInterfaces map[string][]string `json:"network_interfaces,omitempty"`
		Disks             []systemDiskStat    `json:"disks,omitempty"`
		TopProcesses      []map[string]any    `json:"top_processes,omitempty"`
	}
	out := snapshot{
		Timestamp: time.Now().UTC(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
	}
	if hostname, err := os.Hostname(); err == nil {
		out.Hostname = hostname
	}
	if cwd, err := os.Getwd(); err == nil {
		out.WorkingDirectory = cwd
		if stat, err := statDisk(cwd); err == nil {
			out.Disks = append(out.Disks, stat)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out.HomeDirectory = home
		if home != "" && home != out.WorkingDirectory {
			if stat, err := statDisk(home); err == nil {
				out.Disks = append(out.Disks, stat)
			}
		}
	}
	if currentUser, err := user.Current(); err == nil {
		out.CurrentUser = currentUser.Username
	}
	if uptime, err := readUptime(); err == nil {
		out.UptimeSeconds = uptime
	}
	if loads, err := readLoadAverage(); err == nil {
		out.LoadAverage = loads
	}
	if meminfo, err := readMemInfo(); err == nil {
		out.Memory = meminfo
	}
	if network, err := listNetworkInterfaces(); err == nil {
		out.NetworkInterfaces = network
	}
	if input.IncludeProcesses {
		processes, err := listProcesses(ctx, input.ProcessLimit, "")
		if err == nil {
			out.TopProcesses = make([]map[string]any, 0, len(processes))
			for _, process := range processes {
				out.TopProcesses = append(out.TopProcesses, map[string]any{
					"pid":     process.PID,
					"ppid":    process.PPID,
					"pgid":    process.PGID,
					"state":   process.State,
					"cpu_pct": process.CPUPercent,
					"mem_pct": process.MemoryPercent,
					"elapsed": process.Elapsed,
					"command": process.Command,
				})
			}
		}
	}
	return marshalToolJSON(out), nil
}

type FilesystemTool struct {
	cfg config.ToolsConfig
}

func NewFilesystemTool(cfg config.ToolsConfig) *FilesystemTool { return &FilesystemTool{cfg: cfg} }

func (t *FilesystemTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "filesystem",
		Description: "Inspect or modify the local filesystem using structured actions such as list, read, write, append, stat, move, mkdir, and remove. Prefer this over raw shell commands when the task is fundamentally file-oriented. Be especially careful with write, move, and remove because they change local state directly.",
		Parameters: schemaObject(map[string]any{
			"action":      schemaStringEnum("Filesystem action to perform.", "list", "read", "write", "append", "stat", "move", "mkdir", "remove"),
			"path":        schemaString("Path to the file or directory to inspect or modify."),
			"destination": schemaString("Destination path for action=move."),
			"content":     schemaString("Text content to write or append for action=write or action=append."),
			"recursive":   schemaBoolean("Whether mkdir or remove should recurse. For remove, this can delete entire directory trees."),
			"overwrite":   schemaBoolean("Whether write or move may replace an existing destination."),
			"max_bytes":   schemaInteger("Maximum bytes to read back for action=read before truncating the result."),
		}, "action", "path"),
	}
}

func (t *FilesystemTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Action      string `json:"action"`
		Path        string `json:"path"`
		Destination string `json:"destination"`
		Content     string `json:"content"`
		Recursive   bool   `json:"recursive"`
		Overwrite   bool   `json:"overwrite"`
		MaxBytes    int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = t.cfg.MaxCommandBytes
	}
	action := strings.TrimSpace(strings.ToLower(input.Action))
	path, err := resolveLocalPath(input.Path)
	if err != nil {
		return "", err
	}
	writeAction := action == "write" || action == "append" || action == "move" || action == "mkdir" || action == "remove"
	if err := ensurePathAccess(ctx, path, writeAction); err != nil {
		return "", err
	}
	switch action {
	case "list":
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		type entry struct {
			Name      string    `json:"name"`
			Path      string    `json:"path"`
			IsDir     bool      `json:"is_dir"`
			SizeBytes int64     `json:"size_bytes"`
			Mode      string    `json:"mode"`
			Modified  time.Time `json:"modified_at"`
		}
		out := make([]entry, 0, len(entries))
		for _, item := range entries {
			info, err := item.Info()
			if err != nil {
				continue
			}
			out = append(out, entry{
				Name:      item.Name(),
				Path:      filepath.Join(path, item.Name()),
				IsDir:     item.IsDir(),
				SizeBytes: info.Size(),
				Mode:      info.Mode().String(),
				Modified:  info.ModTime().UTC(),
			})
		}
		return marshalToolJSON(map[string]any{"path": path, "entries": out}), nil
	case "read":
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", path)
		}
		rawData, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		truncated := false
		if len(rawData) > input.MaxBytes {
			rawData = rawData[:input.MaxBytes]
			truncated = true
		}
		content := string(rawData)
		encoding := "text"
		if !utf8.Valid(rawData) {
			content = base64.StdEncoding.EncodeToString(rawData)
			encoding = "base64"
		}
		return marshalToolJSON(map[string]any{
			"path":       path,
			"size_bytes": info.Size(),
			"encoding":   encoding,
			"truncated":  truncated,
			"content":    content,
		}), nil
	case "write":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if !input.Overwrite {
			if _, err := os.Stat(path); err == nil {
				return "", fmt.Errorf("%s already exists", path)
			}
		}
		if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"path": path, "written_bytes": len(input.Content), "action": "write"}), nil
	case "append":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		written, err := f.WriteString(input.Content)
		if err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"path": path, "written_bytes": written, "action": "append"}), nil
	case "stat":
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{
			"path":         path,
			"name":         info.Name(),
			"is_dir":       info.IsDir(),
			"size_bytes":   info.Size(),
			"mode":         info.Mode().String(),
			"modified_at":  info.ModTime().UTC(),
			"permissions":  fmt.Sprintf("%#o", info.Mode().Perm()),
			"absolute_dir": filepath.Dir(path),
		}), nil
	case "move":
		dest, err := resolveLocalPath(input.Destination)
		if err != nil {
			return "", err
		}
		if err := ensurePathAccess(ctx, dest, true); err != nil {
			return "", err
		}
		if !input.Overwrite {
			if _, err := os.Stat(dest); err == nil {
				return "", fmt.Errorf("%s already exists", dest)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(path, dest); err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"from": path, "to": dest, "action": "move"}), nil
	case "mkdir":
		mode := os.FileMode(0o755)
		if input.Recursive {
			err = os.MkdirAll(path, mode)
		} else {
			err = os.Mkdir(path, mode)
		}
		if err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"path": path, "action": "mkdir"}), nil
	case "remove":
		if input.Recursive {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"path": path, "action": "remove", "recursive": input.Recursive}), nil
	default:
		return "", fmt.Errorf("unsupported filesystem action %q", input.Action)
	}
}

type ProcessTool struct {
	cfg    config.ToolsConfig
	policy *policy.Engine
}

func NewProcessTool(cfg config.ToolsConfig, engine *policy.Engine) *ProcessTool {
	return &ProcessTool{cfg: cfg, policy: engine}
}

func (t *ProcessTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "manage_process",
		Description: "List, inspect, start, and signal local processes without dropping to ad-hoc shell commands. Prefer action=start for long-running or stateful commands such as apt update, package installs, servers, watchers, and builds that should continue in the background. Use run_command instead only when the job is short and you need the result immediately.",
		Parameters: schemaObject(map[string]any{
			"action":          schemaStringEnum("Process-management action to perform.", "list", "inspect", "start", "signal"),
			"pid":             schemaInteger("Target process id for inspect or signal."),
			"command":         schemaString("Shell command to launch for action=start."),
			"workdir":         schemaString("Optional working directory for action=start."),
			"stdout_path":     schemaString("Optional log file path for captured stdout when starting a process."),
			"stderr_path":     schemaString("Optional log file path for captured stderr when starting a process."),
			"filter":          schemaString("Substring filter applied to command lines when action=list."),
			"limit":           schemaInteger("Maximum number of processes to return when action=list."),
			"signal":          schemaString("Signal name such as TERM or KILL for action=signal."),
			"timeout_seconds": schemaInteger("Reserved for short bounded process-management operations."),
		}, "action"),
	}
}

func (t *ProcessTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Action         string `json:"action"`
		PID            int    `json:"pid"`
		Command        string `json:"command"`
		Workdir        string `json:"workdir"`
		StdoutPath     string `json:"stdout_path"`
		StderrPath     string `json:"stderr_path"`
		Filter         string `json:"filter"`
		Limit          int    `json:"limit"`
		Signal         string `json:"signal"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if input.Limit <= 0 {
		input.Limit = 20
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 10
	}
	action := strings.TrimSpace(strings.ToLower(input.Action))
	switch action {
	case "list":
		if err := ensurePrivilegedSystemControl(ctx); err != nil {
			return "", err
		}
		processes, err := listProcesses(ctx, input.Limit, input.Filter)
		if err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"processes": processes}), nil
	case "inspect":
		if err := ensurePrivilegedSystemControl(ctx); err != nil {
			return "", err
		}
		if input.PID <= 0 {
			return "", fmt.Errorf("pid is required for inspect")
		}
		detail, err := inspectProcess(input.PID)
		if err != nil {
			return "", err
		}
		return marshalToolJSON(detail), nil
	case "start":
		if err := ensurePrivilegedSystemControl(ctx); err != nil {
			return "", err
		}
		if !t.cfg.AllowCommandExecution {
			return "", fmt.Errorf("command execution is disabled")
		}
		command := strings.TrimSpace(input.Command)
		if command == "" {
			return "", fmt.Errorf("command is required for start")
		}
		convo := policyContextFromTool(ctx)
		if t.policy != nil {
			result := t.policy.EvaluateCommandForContext(command, convo)
			if result.Verdict != policy.VerdictAllow {
				return "", fmt.Errorf("process start denied by policy: %s (risk=%s)", result.Reason, result.Risk)
			}
		}
		workdir := ""
		if strings.TrimSpace(input.Workdir) != "" {
			var err error
			workdir, err = resolveLocalPath(input.Workdir)
			if err != nil {
				return "", err
			}
			if err := ensurePathAccess(ctx, workdir, false); err != nil {
				return "", err
			}
		}
		stdoutPath, err := defaultProcessLogPath(input.StdoutPath, "stdout")
		if err != nil {
			return "", err
		}
		stderrPath, err := defaultProcessLogPath(input.StderrPath, "stderr")
		if err != nil {
			return "", err
		}
		if err := ensurePathAccess(ctx, stdoutPath, true); err != nil {
			return "", err
		}
		if err := ensurePathAccess(ctx, stderrPath, true); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(stderrPath), 0o755); err != nil {
			return "", err
		}
		stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", err
		}
		defer stdoutFile.Close()
		stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", err
		}
		defer stderrFile.Close()
		cmd, err := commandenv.ShellCommandContext(context.Background(), t.cfg.CommandShell, command)
		if err != nil {
			return "", err
		}
		if workdir != "" {
			cmd.Dir = workdir
		}
		cmd.Stdout = stdoutFile
		cmd.Stderr = stderrFile
		if runtime.GOOS != "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		if err := cmd.Start(); err != nil {
			return "", err
		}
		pid := cmd.Process.Pid
		_ = cmd.Process.Release()
		return marshalToolJSON(map[string]any{
			"pid":         pid,
			"command":     command,
			"workdir":     workdir,
			"stdout_path": stdoutPath,
			"stderr_path": stderrPath,
			"started_at":  time.Now().UTC(),
		}), nil
	case "signal":
		if err := ensurePrivilegedSystemControl(ctx); err != nil {
			return "", err
		}
		if input.PID <= 0 {
			return "", fmt.Errorf("pid is required for signal")
		}
		sig, err := parseSignal(input.Signal)
		if err != nil {
			return "", err
		}
		process, err := os.FindProcess(input.PID)
		if err != nil {
			return "", err
		}
		if err := process.Signal(sig); err != nil {
			return "", err
		}
		return marshalToolJSON(map[string]any{"pid": input.PID, "signal": signalName(sig), "sent_at": time.Now().UTC()}), nil
	default:
		return "", fmt.Errorf("unsupported process action %q", input.Action)
	}
}

type listedProcess struct {
	PID           int    `json:"pid"`
	PPID          int    `json:"ppid"`
	PGID          int    `json:"pgid"`
	State         string `json:"state"`
	CPUPercent    string `json:"cpu_pct"`
	MemoryPercent string `json:"mem_pct"`
	Elapsed       string `json:"elapsed"`
	Command       string `json:"command"`
}

func marshalToolJSON(value any) string {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return string(raw)
}

func policyContextFromTool(ctx context.Context) types.ConversationContext {
	convo, ok := ConversationContextFrom(ctx)
	if !ok {
		return types.ConversationContext{Trust: types.TrustOwner, IsOwner: true}
	}
	return convo
}

func ensurePrivilegedSystemControl(ctx context.Context) error {
	convo, ok := ConversationContextFrom(ctx)
	if !ok {
		return nil
	}
	if convo.IsOwner || convo.Trust == types.TrustOwner || convo.Trust == types.TrustSystem {
		return nil
	}
	return fmt.Errorf("this action requires owner-level local device control authority")
}

func ensurePathAccess(ctx context.Context, path string, write bool) error {
	convo, ok := ConversationContextFrom(ctx)
	if !ok {
		return nil
	}
	if convo.IsOwner || convo.Trust == types.TrustOwner || convo.Trust == types.TrustSystem {
		return nil
	}
	if write {
		return fmt.Errorf("filesystem write actions require owner context")
	}
	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	allowed, err := pathWithinBase(workspace, path)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("non-owner file access is limited to the workspace")
	}
	return nil
}

func pathWithinBase(base string, target string) (bool, error) {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, err
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."), nil
}

func resolveLocalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, path)), nil
}

func defaultProcessLogPath(path string, stream string) (string, error) {
	if strings.TrimSpace(path) != "" {
		return resolveLocalPath(path)
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("qorvexus-%s-%d.log", stream, time.Now().UTC().UnixNano())), nil
}

func readUptime() (float64, error) {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("unexpected /proc/uptime format")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readLoadAverage() (map[string]float64, error) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected /proc/loadavg format")
	}
	one, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}
	five, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, err
	}
	fifteen, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, err
	}
	return map[string]float64{"1m": one, "5m": five, "15m": fifteen}, nil
}

func readMemInfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out[key] = value * 1024
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func listNetworkInterfaces() (map[string][]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			out[iface.Name] = append(out[iface.Name], addr.String())
		}
	}
	return out, nil
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

func listProcesses(ctx context.Context, limit int, filter string) ([]listedProcess, error) {
	cmd, err := commandenv.CommandContext(ctx, "ps", "-eo", "pid=,ppid=,pgid=,stat=,%cpu=,%mem=,etime=,command=", "--sort=-%cpu")
	if err != nil {
		return nil, err
	}
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	out := make([]listedProcess, 0, minInt(limit, len(lines)))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		command := strings.Join(fields[7:], " ")
		if filter != "" && !strings.Contains(strings.ToLower(command), filter) {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, listedProcess{
			PID:           pid,
			PPID:          ppid,
			PGID:          pgid,
			State:         fields[3],
			CPUPercent:    fields[4],
			MemoryPercent: fields[5],
			Elapsed:       fields[6],
			Command:       command,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func inspectProcess(pid int) (map[string]any, error) {
	detail := map[string]any{"pid": pid}
	if summary, err := inspectProcessSummary(pid); err == nil {
		detail["summary"] = summary
	}
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		detail["cwd"] = cwd
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		detail["exe"] = exe
	}
	if status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		detail["status"] = parseProcStatus(string(status))
	}
	if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		clean := strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
		if clean != "" {
			detail["cmdline"] = clean
		}
	}
	if len(detail) == 1 {
		return nil, fmt.Errorf("process %d not found", pid)
	}
	return detail, nil
}

func inspectProcessSummary(pid int) (listedProcess, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,pgid=,stat=,%cpu=,%mem=,etime=,command=")
	raw, err := cmd.Output()
	if err != nil {
		return listedProcess{}, err
	}
	line := strings.TrimSpace(string(raw))
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return listedProcess{}, fmt.Errorf("unexpected ps output for process %d", pid)
	}
	ppid, _ := strconv.Atoi(fields[1])
	pgid, _ := strconv.Atoi(fields[2])
	return listedProcess{
		PID:           pid,
		PPID:          ppid,
		PGID:          pgid,
		State:         fields[3],
		CPUPercent:    fields[4],
		MemoryPercent: fields[5],
		Elapsed:       fields[6],
		Command:       strings.Join(fields[7:], " "),
	}, nil
}

func parseProcStatus(raw string) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}

func parseSignal(value string) (syscall.Signal, error) {
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
		return 0, fmt.Errorf("unsupported signal %q", value)
	}
}

func signalName(sig syscall.Signal) string {
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
		return fmt.Sprintf("SIGNAL(%d)", sig)
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
