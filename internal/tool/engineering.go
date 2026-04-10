package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"qorvexus/internal/commandenv"
	"qorvexus/internal/config"
	"qorvexus/internal/policy"
	"qorvexus/internal/types"
)

type RepoIndexTool struct{}

type RepoSearchTool struct {
	cfg config.ToolsConfig
}

type ApplyDiffTool struct {
	cfg config.ToolsConfig
}

type ChangeSummaryTool struct {
	cfg config.ToolsConfig
}

type TestFailureLocatorTool struct {
	cfg    config.ToolsConfig
	policy *policy.Engine
}

func NewRepoIndexTool() *RepoIndexTool { return &RepoIndexTool{} }

func NewRepoSearchTool(cfg config.ToolsConfig) *RepoSearchTool {
	return &RepoSearchTool{cfg: cfg}
}

func NewApplyDiffTool(cfg config.ToolsConfig) *ApplyDiffTool {
	return &ApplyDiffTool{cfg: cfg}
}

func NewChangeSummaryTool(cfg config.ToolsConfig) *ChangeSummaryTool {
	return &ChangeSummaryTool{cfg: cfg}
}

func NewTestFailureLocatorTool(cfg config.ToolsConfig, engine *policy.Engine) *TestFailureLocatorTool {
	return &TestFailureLocatorTool{cfg: cfg, policy: engine}
}

func (t *RepoIndexTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "repo_index",
		Description: "Scan a repository or project directory and return a structured engineering index: git metadata, language mix, key files, top directories, and overall file inventory. Prefer this early when you need fast orientation before searching or editing.",
		Parameters: schemaObject(map[string]any{
			"path":      schemaString("Repository or project directory to scan. Defaults to the current workspace when omitted."),
			"max_files": schemaInteger("Maximum number of files to inspect before stopping."),
		}),
	}
}

func (t *RepoIndexTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path     string `json:"path"`
		MaxFiles int    `json:"max_files"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	root, err := resolveRepoPath(input.Path)
	if err != nil {
		return "", err
	}
	if err := ensurePathAccess(ctx, root, false); err != nil {
		return "", err
	}
	if input.MaxFiles <= 0 {
		input.MaxFiles = 5000
	}
	index, err := buildRepoIndex(root, input.MaxFiles)
	if err != nil {
		return "", err
	}
	return marshalToolJSON(index), nil
}

func (t *RepoSearchTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "repo_search",
		Description: "Search a codebase structurally by file name or file content and return line-aware matches with context instead of plain grep text. Prefer this over run_command grep output when you want model-friendly search results that are easier to reason over.",
		Parameters: schemaObject(map[string]any{
			"query":          schemaString("Search text or regex pattern."),
			"path":           schemaString("Repository root or subdirectory to search."),
			"mode":           schemaStringEnum("Whether to search file content, filenames, or both.", "content", "filename", "both"),
			"regex":          schemaBoolean("Interpret query as a regular expression instead of plain text."),
			"case_sensitive": schemaBoolean("Whether matching should respect letter case."),
			"glob":           schemaString("Optional glob filter such as *.go or internal/**."),
			"file_extensions": schemaArray("Optional file-extension allowlist such as [\"go\", \"md\"].",
				schemaString("File extension without the dot.")),
			"limit":          schemaInteger("Maximum number of matches to return."),
			"context_lines":  schemaInteger("How many surrounding lines to include around each content match."),
			"max_file_bytes": schemaInteger("Skip or cap very large files beyond this size."),
		}, "query"),
	}
}

func (t *RepoSearchTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Query          string   `json:"query"`
		Path           string   `json:"path"`
		Mode           string   `json:"mode"`
		Regex          bool     `json:"regex"`
		CaseSensitive  bool     `json:"case_sensitive"`
		Glob           string   `json:"glob"`
		FileExtensions []string `json:"file_extensions"`
		Limit          int      `json:"limit"`
		ContextLines   int      `json:"context_lines"`
		MaxFileBytes   int      `json:"max_file_bytes"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	root, err := resolveRepoPath(input.Path)
	if err != nil {
		return "", err
	}
	if err := ensurePathAccess(ctx, root, false); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Mode) == "" {
		input.Mode = "content"
	}
	if input.Limit <= 0 {
		input.Limit = 25
	}
	if input.ContextLines < 0 {
		input.ContextLines = 0
	}
	if input.MaxFileBytes <= 0 {
		input.MaxFileBytes = minInt(t.cfg.MaxCommandBytes, 256*1024)
	}
	searcher, err := newRepoSearcher(input.Query, input.Regex, input.CaseSensitive)
	if err != nil {
		return "", err
	}
	report, err := searchRepo(root, repoSearchOptions{
		Mode:           strings.TrimSpace(strings.ToLower(input.Mode)),
		Glob:           strings.TrimSpace(input.Glob),
		FileExtensions: input.FileExtensions,
		Limit:          input.Limit,
		ContextLines:   input.ContextLines,
		MaxFileBytes:   input.MaxFileBytes,
		Searcher:       searcher,
	})
	if err != nil {
		return "", err
	}
	return marshalToolJSON(report), nil
}

func (t *ApplyDiffTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "apply_diff",
		Description: "Apply a unified diff or git-style patch to the local repository and return a structured summary of affected files. Prefer this for targeted code edits when you already know the patch to make. Use check_only first when you want to validate applicability before mutating the worktree.",
		Parameters: schemaObject(map[string]any{
			"patch":      schemaString("Unified diff or git-style patch text to apply."),
			"path":       schemaString("Repository path to treat as the patch root. Defaults to the current workspace."),
			"check_only": schemaBoolean("Validate whether the patch can apply cleanly without changing files."),
			"reverse":    schemaBoolean("Apply the patch in reverse, effectively undoing it if possible."),
		}, "patch"),
	}
}

func (t *ApplyDiffTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Patch     string `json:"patch"`
		Path      string `json:"path"`
		CheckOnly bool   `json:"check_only"`
		Reverse   bool   `json:"reverse"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Patch) == "" {
		return "", fmt.Errorf("patch is required")
	}
	root, err := resolveRepoPath(input.Path)
	if err != nil {
		return "", err
	}
	if err := ensurePathAccess(ctx, root, true); err != nil {
		return "", err
	}
	repoRoot, err := gitRoot(root)
	if err != nil {
		return "", fmt.Errorf("apply_diff requires a git worktree: %w", err)
	}
	for _, target := range parsePatchTargets(input.Patch) {
		if strings.TrimSpace(target) == "" {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.Clean(target))
		allowed, err := pathWithinBase(repoRoot, abs)
		if err != nil {
			return "", err
		}
		if !allowed {
			return "", fmt.Errorf("patch target %q escapes repository root", target)
		}
	}
	tempPatch, err := os.CreateTemp("", "qorvexus-apply-*.diff")
	if err != nil {
		return "", err
	}
	tempPatchPath := tempPatch.Name()
	defer os.Remove(tempPatchPath)
	if _, err := tempPatch.WriteString(input.Patch); err != nil {
		_ = tempPatch.Close()
		return "", err
	}
	if err := tempPatch.Close(); err != nil {
		return "", err
	}
	args := []string{"-C", repoRoot, "apply", "--recount", "--whitespace=nowarn"}
	if input.CheckOnly {
		args = append(args, "--check")
	}
	if input.Reverse {
		args = append(args, "--reverse")
	}
	args = append(args, tempPatchPath)
	cmd, err := commandenv.CommandContext(ctx, "git", args...)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if runErr != nil {
		if output == "" {
			output = runErr.Error()
		}
		return "", fmt.Errorf("apply diff: %s", strings.TrimSpace(output))
	}
	report := map[string]any{
		"repo_root":  repoRoot,
		"check_only": input.CheckOnly,
		"reverse":    input.Reverse,
		"files":      parsePatchTargets(input.Patch),
	}
	if strings.TrimSpace(output) != "" {
		report["output"] = output
	}
	if !input.CheckOnly {
		summary, err := summarizeGitChanges(ctx, repoRoot, nil, "", 25)
		if err == nil {
			report["change_summary"] = summary
		}
	}
	return marshalToolJSON(report), nil
}

func (t *ChangeSummaryTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "summarize_changes",
		Description: "Summarize the current repository diff in a structured way, including changed files, statuses, line counts, and edited hunk locations. Prefer this when you need to review or explain worktree changes without dumping raw git diff text.",
		Parameters: schemaObject(map[string]any{
			"path":      schemaString("Repository path whose diff should be summarized."),
			"paths":     schemaArray("Optional subset of file paths to summarize.", schemaString("Path relative to the repository root.")),
			"base_ref":  schemaString("Optional git base ref to compare against instead of the default worktree diff."),
			"max_files": schemaInteger("Maximum number of changed files to include in the summary."),
		}),
	}
}

func (t *ChangeSummaryTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path     string   `json:"path"`
		Paths    []string `json:"paths"`
		BaseRef  string   `json:"base_ref"`
		MaxFiles int      `json:"max_files"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	root, err := resolveRepoPath(input.Path)
	if err != nil {
		return "", err
	}
	if err := ensurePathAccess(ctx, root, false); err != nil {
		return "", err
	}
	if input.MaxFiles <= 0 {
		input.MaxFiles = 50
	}
	report, err := summarizeGitChanges(ctx, root, input.Paths, input.BaseRef, input.MaxFiles)
	if err != nil {
		return "", err
	}
	return marshalToolJSON(report), nil
}

func (t *TestFailureLocatorTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "locate_test_failure",
		Description: "Parse failing test output, or run a test command, and localize likely source files, lines, and snippets that explain the failure. Prefer passing existing output when you already have it; use command only when the tool should execute the test itself.",
		Parameters: schemaObject(map[string]any{
			"command":          schemaString("Optional test command to run, such as go test ./..."),
			"output":           schemaString("Existing failing test output to analyze directly without rerunning tests."),
			"workdir":          schemaString("Repository directory where the command should run or where source lookup should happen."),
			"timeout_seconds":  schemaInteger("Timeout for command execution when command is provided."),
			"context_lines":    schemaInteger("How many surrounding source lines to include around localized failures."),
			"max_output_bytes": schemaInteger("Maximum bytes of command output to keep before truncating."),
		}),
	}
}

func (t *TestFailureLocatorTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Command        string `json:"command"`
		Output         string `json:"output"`
		Workdir        string `json:"workdir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		ContextLines   int    `json:"context_lines"`
		MaxOutputBytes int    `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Command) == "" && strings.TrimSpace(input.Output) == "" {
		return "", fmt.Errorf("command or output is required")
	}
	workdir, err := resolveRepoPath(input.Workdir)
	if err != nil {
		return "", err
	}
	if err := ensurePathAccess(ctx, workdir, false); err != nil {
		return "", err
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 60
	}
	if input.ContextLines < 0 {
		input.ContextLines = 2
	}
	if input.MaxOutputBytes <= 0 {
		input.MaxOutputBytes = t.cfg.MaxCommandBytes
	}
	output := strings.TrimSpace(input.Output)
	exitCode := 0
	commandTimedOut := false
	commandRan := false
	if strings.TrimSpace(input.Command) != "" {
		commandRan = true
		if !t.cfg.AllowCommandExecution {
			return "", fmt.Errorf("command execution is disabled")
		}
		policyCtx := policyContextFromTool(ctx)
		if t.policy != nil {
			result := t.policy.EvaluateCommandForContext(input.Command, policyCtx)
			if result.Verdict != policy.VerdictAllow {
				return "", fmt.Errorf("test command denied by policy: %s (risk=%s)", result.Reason, result.Risk)
			}
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer cancel()
		cmd, err := commandenv.ShellCommandContext(runCtx, t.cfg.CommandShell, input.Command)
		if err != nil {
			return "", err
		}
		cmd.Dir = workdir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		output = stdout.String()
		if serr := strings.TrimSpace(stderr.String()); serr != "" {
			if output != "" {
				output += "\n"
			}
			output += serr
		}
		if len(output) > input.MaxOutputBytes {
			output = output[:input.MaxOutputBytes] + "\n[truncated]"
		}
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else if errors.Is(err, context.DeadlineExceeded) || runCtx.Err() == context.DeadlineExceeded {
				commandTimedOut = true
				exitCode = -1
			} else {
				return "", err
			}
		}
	}
	report, err := localizeTestFailure(workdir, output, input.ContextLines)
	if err != nil {
		return "", err
	}
	report["command"] = strings.TrimSpace(input.Command)
	report["command_ran"] = commandRan
	report["exit_code"] = exitCode
	report["timed_out"] = commandTimedOut
	report["raw_output_excerpt"] = truncateEngineeringText(output, minInt(input.MaxOutputBytes, 4000))
	return marshalToolJSON(report), nil
}

type repoIndexLanguage struct {
	Language   string `json:"language"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type repoIndexDir struct {
	Path       string `json:"path"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type repoIndexReport struct {
	Root           string              `json:"root"`
	GitRoot        string              `json:"git_root,omitempty"`
	IsGitRepo      bool                `json:"is_git_repo"`
	Branch         string              `json:"branch,omitempty"`
	Head           string              `json:"head,omitempty"`
	FileCount      int                 `json:"file_count"`
	DirectoryCount int                 `json:"directory_count"`
	TotalBytes     int64               `json:"total_bytes"`
	Truncated      bool                `json:"truncated,omitempty"`
	Languages      []repoIndexLanguage `json:"languages,omitempty"`
	TopDirectories []repoIndexDir      `json:"top_directories,omitempty"`
	KeyFiles       []string            `json:"key_files,omitempty"`
}

var errStopWalk = errors.New("stop walk")

func buildRepoIndex(root string, maxFiles int) (repoIndexReport, error) {
	report := repoIndexReport{Root: root}
	if gitRoot, err := gitRoot(root); err == nil {
		report.IsGitRepo = true
		report.GitRoot = gitRoot
		report.Branch, _ = gitRevParse(gitRoot, "--abbrev-ref", "HEAD")
		report.Head, _ = gitRevParse(gitRoot, "HEAD")
	}
	type langStats struct {
		files int
		bytes int64
	}
	type dirStats struct {
		files int
		bytes int64
	}
	languageTotals := map[string]*langStats{}
	dirTotals := map[string]*dirStats{}
	keyFiles := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			report.DirectoryCount++
			return nil
		}
		if report.FileCount >= maxFiles {
			report.Truncated = true
			return errStopWalk
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		report.FileCount++
		report.TotalBytes += info.Size()
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		topDir := rel
		if idx := strings.IndexRune(rel, filepath.Separator); idx >= 0 {
			topDir = rel[:idx]
		} else {
			topDir = "."
		}
		stats := dirTotals[topDir]
		if stats == nil {
			stats = &dirStats{}
			dirTotals[topDir] = stats
		}
		stats.files++
		stats.bytes += info.Size()
		if lang := detectLanguage(rel); lang != "" {
			stats := languageTotals[lang]
			if stats == nil {
				stats = &langStats{}
				languageTotals[lang] = stats
			}
			stats.files++
			stats.bytes += info.Size()
		}
		if isKeyRepoFile(rel) {
			keyFiles[filepath.ToSlash(rel)] = struct{}{}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return repoIndexReport{}, err
	}
	for lang, stats := range languageTotals {
		report.Languages = append(report.Languages, repoIndexLanguage{
			Language:   lang,
			FileCount:  stats.files,
			TotalBytes: stats.bytes,
		})
	}
	sort.Slice(report.Languages, func(i, j int) bool {
		if report.Languages[i].FileCount == report.Languages[j].FileCount {
			return report.Languages[i].Language < report.Languages[j].Language
		}
		return report.Languages[i].FileCount > report.Languages[j].FileCount
	})
	for path, stats := range dirTotals {
		report.TopDirectories = append(report.TopDirectories, repoIndexDir{
			Path:       filepath.ToSlash(path),
			FileCount:  stats.files,
			TotalBytes: stats.bytes,
		})
	}
	sort.Slice(report.TopDirectories, func(i, j int) bool {
		if report.TopDirectories[i].FileCount == report.TopDirectories[j].FileCount {
			return report.TopDirectories[i].Path < report.TopDirectories[j].Path
		}
		return report.TopDirectories[i].FileCount > report.TopDirectories[j].FileCount
	})
	if len(report.TopDirectories) > 12 {
		report.TopDirectories = report.TopDirectories[:12]
	}
	for path := range keyFiles {
		report.KeyFiles = append(report.KeyFiles, path)
	}
	sort.Strings(report.KeyFiles)
	return report, nil
}

type repoSearcher struct {
	query         string
	regex         bool
	caseSensitive bool
	re            *regexp.Regexp
}

func newRepoSearcher(query string, regex bool, caseSensitive bool) (*repoSearcher, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	searcher := &repoSearcher{
		query:         query,
		regex:         regex,
		caseSensitive: caseSensitive,
	}
	if regex {
		pattern := query
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		searcher.re = re
	}
	return searcher, nil
}

func (s *repoSearcher) matchesText(value string) bool {
	if s.regex {
		return s.re.MatchString(value)
	}
	if s.caseSensitive {
		return strings.Contains(value, s.query)
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(s.query))
}

func (s *repoSearcher) matchIndexes(value string) [][]int {
	if s.regex {
		return s.re.FindAllStringIndex(value, -1)
	}
	haystack := value
	needle := s.query
	if !s.caseSensitive {
		haystack = strings.ToLower(value)
		needle = strings.ToLower(s.query)
	}
	var matches [][]int
	start := 0
	for {
		idx := strings.Index(haystack[start:], needle)
		if idx == -1 {
			break
		}
		begin := start + idx
		matches = append(matches, []int{begin, begin + len(needle)})
		start = begin + len(needle)
		if len(needle) == 0 {
			break
		}
	}
	return matches
}

type repoSearchOptions struct {
	Mode           string
	Glob           string
	FileExtensions []string
	Limit          int
	ContextLines   int
	MaxFileBytes   int
	Searcher       *repoSearcher
}

type repoSearchMatch struct {
	Path          string   `json:"path"`
	Kind          string   `json:"kind"`
	Line          int      `json:"line,omitempty"`
	Column        int      `json:"column,omitempty"`
	Preview       string   `json:"preview"`
	ContextBefore []string `json:"context_before,omitempty"`
	ContextAfter  []string `json:"context_after,omitempty"`
	Language      string   `json:"language,omitempty"`
}

type repoSearchReport struct {
	Root          string            `json:"root"`
	Query         string            `json:"query"`
	Mode          string            `json:"mode"`
	ScannedFiles  int               `json:"scanned_files"`
	MatchedFiles  int               `json:"matched_files"`
	SkippedBinary int               `json:"skipped_binary_files,omitempty"`
	SkippedLarge  int               `json:"skipped_large_files,omitempty"`
	Truncated     bool              `json:"truncated,omitempty"`
	Results       []repoSearchMatch `json:"results"`
}

func searchRepo(root string, opts repoSearchOptions) (repoSearchReport, error) {
	report := repoSearchReport{
		Root:  root,
		Query: opts.Searcher.query,
		Mode:  opts.Mode,
	}
	extFilter := map[string]struct{}{}
	for _, ext := range opts.FileExtensions {
		ext = strings.TrimSpace(strings.ToLower(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extFilter[ext] = struct{}{}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		rel = filepath.ToSlash(rel)
		if opts.Glob != "" {
			matched, err := filepath.Match(opts.Glob, rel)
			if err != nil {
				return err
			}
			if !matched {
				return nil
			}
		}
		if len(extFilter) > 0 {
			if _, ok := extFilter[strings.ToLower(filepath.Ext(rel))]; !ok {
				return nil
			}
		}
		report.ScannedFiles++
		matchedThisFile := false
		if opts.Mode == "filename" || opts.Mode == "both" {
			if opts.Searcher.matchesText(rel) {
				report.Results = append(report.Results, repoSearchMatch{
					Path:     rel,
					Kind:     "filename",
					Preview:  rel,
					Language: detectLanguage(rel),
				})
				matchedThisFile = true
			}
		}
		if len(report.Results) >= opts.Limit {
			report.Truncated = true
			return errStopWalk
		}
		if opts.Mode != "filename" {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Size() > int64(opts.MaxFileBytes) {
				report.SkippedLarge++
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if !looksLikeText(raw) {
				report.SkippedBinary++
				return nil
			}
			lines := splitLines(string(raw))
			for i, line := range lines {
				indexes := opts.Searcher.matchIndexes(line)
				if len(indexes) == 0 {
					continue
				}
				for _, idx := range indexes {
					report.Results = append(report.Results, repoSearchMatch{
						Path:          rel,
						Kind:          "content",
						Line:          i + 1,
						Column:        idx[0] + 1,
						Preview:       strings.TrimSpace(line),
						ContextBefore: sliceContext(lines, i-opts.ContextLines, i),
						ContextAfter:  sliceContext(lines, i+1, i+1+opts.ContextLines),
						Language:      detectLanguage(rel),
					})
					matchedThisFile = true
					if len(report.Results) >= opts.Limit {
						report.Truncated = true
						break
					}
				}
				if report.Truncated {
					break
				}
			}
		}
		if matchedThisFile {
			report.MatchedFiles++
		}
		if report.Truncated {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return repoSearchReport{}, err
	}
	return report, nil
}

type changeSummaryReport struct {
	RepoRoot       string                  `json:"repo_root"`
	Branch         string                  `json:"branch,omitempty"`
	Head           string                  `json:"head,omitempty"`
	BaseRef        string                  `json:"base_ref,omitempty"`
	FilesChanged   int                     `json:"files_changed"`
	TotalAdditions int                     `json:"total_additions"`
	TotalDeletions int                     `json:"total_deletions"`
	Files          []changeSummaryFileInfo `json:"files"`
}

type changeSummaryFileInfo struct {
	Path           string     `json:"path"`
	OriginalPath   string     `json:"original_path,omitempty"`
	Status         string     `json:"status"`
	StagedStatus   string     `json:"staged_status,omitempty"`
	UnstagedStatus string     `json:"unstaged_status,omitempty"`
	Additions      int        `json:"additions,omitempty"`
	Deletions      int        `json:"deletions,omitempty"`
	Hunks          []diffHunk `json:"hunks,omitempty"`
	UntrackedSize  int64      `json:"untracked_size_bytes,omitempty"`
}

type diffHunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Context  string `json:"context,omitempty"`
}

func summarizeGitChanges(ctx context.Context, root string, paths []string, baseRef string, maxFiles int) (changeSummaryReport, error) {
	repoRoot, err := gitRoot(root)
	if err != nil {
		return changeSummaryReport{}, fmt.Errorf("summarize_changes requires a git worktree: %w", err)
	}
	report := changeSummaryReport{RepoRoot: repoRoot, BaseRef: strings.TrimSpace(baseRef)}
	report.Branch, _ = gitRevParse(repoRoot, "--abbrev-ref", "HEAD")
	report.Head, _ = gitRevParse(repoRoot, "HEAD")

	pathArgs, err := normalizeGitPaths(repoRoot, paths)
	if err != nil {
		return changeSummaryReport{}, err
	}
	fileMap := map[string]*changeSummaryFileInfo{}

	if report.BaseRef != "" {
		if err := populateChangeSummaryAgainstBase(ctx, repoRoot, report.BaseRef, pathArgs, fileMap); err != nil {
			return changeSummaryReport{}, err
		}
	} else {
		if err := populateWorktreeChangeSummary(ctx, repoRoot, pathArgs, fileMap); err != nil {
			return changeSummaryReport{}, err
		}
	}

	keys := make([]string, 0, len(fileMap))
	for key := range fileMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := *fileMap[key]
		report.Files = append(report.Files, item)
		report.TotalAdditions += item.Additions
		report.TotalDeletions += item.Deletions
	}
	if maxFiles > 0 && len(report.Files) > maxFiles {
		report.Files = report.Files[:maxFiles]
	}
	report.FilesChanged = len(report.Files)
	return report, nil
}

func populateWorktreeChangeSummary(ctx context.Context, repoRoot string, pathArgs []string, fileMap map[string]*changeSummaryFileInfo) error {
	args := []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
	if len(pathArgs) > 0 {
		args = append(args, "--")
		args = append(args, pathArgs...)
	}
	raw, err := gitOutput(ctx, repoRoot, args...)
	if err != nil {
		return err
	}
	records := bytes.Split([]byte(raw), []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		if len(record) < 3 {
			continue
		}
		xy := record[:2]
		path := strings.TrimSpace(record[3:])
		original := ""
		if strings.ContainsAny(string(xy[0])+string(xy[1]), "RC") && i+1 < len(records) {
			original = path
			i++
			path = string(records[i])
		}
		item := fileMap[path]
		if item == nil {
			item = &changeSummaryFileInfo{Path: filepath.ToSlash(path)}
			fileMap[path] = item
		}
		item.StagedStatus = strings.TrimSpace(string(xy[0]))
		item.UnstagedStatus = strings.TrimSpace(string(xy[1]))
		item.Status = classifyGitStatus(xy)
		if original != "" {
			item.OriginalPath = filepath.ToSlash(original)
		}
	}
	for path, item := range fileMap {
		if item.Status == "untracked" {
			fullPath := filepath.Join(repoRoot, path)
			info, err := os.Stat(fullPath)
			if err == nil {
				item.UntrackedSize = info.Size()
				item.Additions = countLinesInFile(fullPath)
			}
			continue
		}
		add, del, err := diffNumstat(ctx, repoRoot, false, path)
		if err == nil {
			item.Additions += add
			item.Deletions += del
		}
		add, del, err = diffNumstat(ctx, repoRoot, true, path)
		if err == nil {
			item.Additions += add
			item.Deletions += del
		}
		hunks, _ := diffHunksForPath(ctx, repoRoot, false, path)
		cachedHunks, _ := diffHunksForPath(ctx, repoRoot, true, path)
		item.Hunks = append(hunks, cachedHunks...)
	}
	return nil
}

func populateChangeSummaryAgainstBase(ctx context.Context, repoRoot string, baseRef string, pathArgs []string, fileMap map[string]*changeSummaryFileInfo) error {
	args := []string{"diff", "--name-status", baseRef}
	if len(pathArgs) > 0 {
		args = append(args, "--")
		args = append(args, pathArgs...)
	}
	out, err := gitOutput(ctx, repoRoot, args...)
	if err != nil {
		return err
	}
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		statusCode := fields[0]
		path := fields[len(fields)-1]
		item := &changeSummaryFileInfo{
			Path:         filepath.ToSlash(path),
			Status:       classifyDiffStatus(statusCode),
			StagedStatus: statusCode,
		}
		if strings.HasPrefix(statusCode, "R") && len(fields) >= 3 {
			item.OriginalPath = filepath.ToSlash(fields[1])
		}
		add, del, _ := diffNumstatAgainstBase(ctx, repoRoot, baseRef, path)
		item.Additions = add
		item.Deletions = del
		item.Hunks, _ = diffHunksAgainstBase(ctx, repoRoot, baseRef, path)
		fileMap[path] = item
	}
	return nil
}

func diffNumstat(ctx context.Context, repoRoot string, cached bool, path string) (int, int, error) {
	args := []string{"diff", "--numstat"}
	if cached {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := gitOutput(ctx, repoRoot, args...)
	if err != nil {
		return 0, 0, err
	}
	return parseNumstat(out)
}

func diffNumstatAgainstBase(ctx context.Context, repoRoot string, baseRef string, path string) (int, int, error) {
	out, err := gitOutput(ctx, repoRoot, "diff", "--numstat", baseRef, "--", path)
	if err != nil {
		return 0, 0, err
	}
	return parseNumstat(out)
}

func diffHunksForPath(ctx context.Context, repoRoot string, cached bool, path string) ([]diffHunk, error) {
	args := []string{"diff", "--unified=0"}
	if cached {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := gitOutput(ctx, repoRoot, args...)
	if err != nil {
		return nil, err
	}
	return parseDiffHunks(out), nil
}

func diffHunksAgainstBase(ctx context.Context, repoRoot string, baseRef string, path string) ([]diffHunk, error) {
	out, err := gitOutput(ctx, repoRoot, "diff", "--unified=0", baseRef, "--", path)
	if err != nil {
		return nil, err
	}
	return parseDiffHunks(out), nil
}

func parseNumstat(raw string) (int, int, error) {
	for _, line := range splitLines(raw) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		add, err := strconv.Atoi(fields[0])
		if err != nil {
			add = 0
		}
		del, err := strconv.Atoi(fields[1])
		if err != nil {
			del = 0
		}
		return add, del, nil
	}
	return 0, 0, nil
}

var hunkHeaderRe = regexp.MustCompile(`@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

func parseDiffHunks(raw string) []diffHunk {
	var hunks []diffHunk
	for _, line := range splitLines(raw) {
		m := hunkHeaderRe.FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}
		hunks = append(hunks, diffHunk{
			OldStart: atoiDefault(m[1], 0),
			OldLines: atoiDefault(defaultString(m[2], "1"), 1),
			NewStart: atoiDefault(m[3], 0),
			NewLines: atoiDefault(defaultString(m[4], "1"), 1),
			Context:  strings.TrimSpace(m[5]),
		})
	}
	return hunks
}

var (
	goFailHeaderRe = regexp.MustCompile(`^--- FAIL: ([^\s]+)`)
	pytestCaseRe   = regexp.MustCompile(`^([^\n:]+\.py)::([^\s]+)`)
	fileRefRe      = regexp.MustCompile(`([A-Za-z0-9_./\\-]+\.(?:go|py|js|jsx|ts|tsx|java|rb|rs|c|cc|cpp|h|hpp|swift|kt|m|mm|php)):(\d+)(?::(\d+))?`)
)

func localizeTestFailure(workdir string, output string, contextLines int) (map[string]any, error) {
	framework := detectTestFramework(output)
	changedFiles := map[string]struct{}{}
	if repoRoot, err := gitRoot(workdir); err == nil {
		if summary, err := summarizeGitChanges(context.Background(), repoRoot, nil, "", 200); err == nil {
			for _, file := range summary.Files {
				changedFiles[file.Path] = struct{}{}
			}
		}
	}
	type failureKey struct {
		path   string
		line   int
		column int
		test   string
	}
	type localizedFailure struct {
		Test    string   `json:"test,omitempty"`
		Path    string   `json:"path"`
		Line    int      `json:"line"`
		Column  int      `json:"column,omitempty"`
		Message string   `json:"message"`
		Snippet []string `json:"snippet,omitempty"`
		Changed bool     `json:"changed,omitempty"`
		Hits    int      `json:"hits"`
	}
	failures := map[failureKey]*localizedFailure{}
	currentGoTest := ""
	currentPyTest := ""
	for _, line := range splitLines(output) {
		if m := goFailHeaderRe.FindStringSubmatch(line); len(m) > 0 {
			currentGoTest = m[1]
		}
		if m := pytestCaseRe.FindStringSubmatch(line); len(m) > 0 {
			currentPyTest = m[2]
		}
		matches := fileRefRe.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			path, ok := resolveFailurePath(workdir, match[1])
			if !ok {
				continue
			}
			rel := path
			if relToRoot, err := filepath.Rel(workdir, path); err == nil && !strings.HasPrefix(relToRoot, "..") {
				rel = filepath.ToSlash(relToRoot)
			}
			lineNumber := atoiDefault(match[2], 0)
			column := atoiDefault(match[3], 0)
			testName := currentGoTest
			if testName == "" {
				testName = currentPyTest
			}
			key := failureKey{path: rel, line: lineNumber, column: column, test: testName}
			entry := failures[key]
			if entry == nil {
				entry = &localizedFailure{
					Test:    testName,
					Path:    rel,
					Line:    lineNumber,
					Column:  column,
					Message: strings.TrimSpace(line),
					Changed: containsPath(changedFiles, rel),
				}
				entry.Snippet, _ = readSnippet(path, lineNumber, contextLines)
				failures[key] = entry
			}
			entry.Hits++
		}
	}
	items := make([]localizedFailure, 0, len(failures))
	for _, item := range failures {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Hits == items[j].Hits {
			if items[i].Changed == items[j].Changed {
				if items[i].Path == items[j].Path {
					return items[i].Line < items[j].Line
				}
				return items[i].Path < items[j].Path
			}
			return items[i].Changed
		}
		return items[i].Hits > items[j].Hits
	})
	report := map[string]any{
		"framework": framework,
		"failures":  items,
	}
	if len(items) > 0 {
		report["primary_suspect"] = items[0]
	}
	return report, nil
}

func detectTestFramework(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(output, "--- FAIL:"):
		return "go test"
	case strings.Contains(lower, "pytest") || strings.Contains(output, "::") && strings.Contains(lower, "failed"):
		return "pytest"
	case strings.Contains(output, "Test Suites:") || strings.Contains(output, "FAIL ") && strings.Contains(output, "●"):
		return "jest"
	default:
		return "generic"
	}
}

func resolveFailurePath(workdir string, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	value = strings.ReplaceAll(value, "\\", string(filepath.Separator))
	if filepath.IsAbs(value) {
		if _, err := os.Stat(value); err == nil {
			return filepath.Clean(value), true
		}
		return "", false
	}
	candidate := filepath.Join(workdir, value)
	if _, err := os.Stat(candidate); err == nil {
		return filepath.Clean(candidate), true
	}
	return "", false
}

func readSnippet(path string, lineNumber int, contextLines int) ([]string, error) {
	if lineNumber <= 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(raw))
	if lineNumber > len(lines) {
		lineNumber = len(lines)
	}
	start := maxInt(1, lineNumber-contextLines)
	end := minInt(len(lines), lineNumber+contextLines)
	out := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	return out, nil
}

func gitRoot(path string) (string, error) {
	out, err := gitOutput(context.Background(), path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitRevParse(repoRoot string, args ...string) (string, error) {
	out, err := gitOutput(context.Background(), repoRoot, append([]string{"rev-parse"}, args...)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmd, err := commandenv.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return stdout.String(), nil
}

func resolveRepoPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	return resolveLocalPath(path)
}

func normalizeGitPaths(repoRoot string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, value := range paths {
		if strings.TrimSpace(value) == "" {
			continue
		}
		abs, err := resolvePathWithinBase(repoRoot, value)
		if err != nil {
			return nil, err
		}
		allowed, err := pathWithinBase(repoRoot, abs)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("path %q is outside the repository root", value)
		}
		rel, err := filepath.Rel(repoRoot, abs)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

func resolvePathWithinBase(base string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(home, value[2:])), nil
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	return filepath.Clean(filepath.Join(base, value)), nil
}

func parsePatchTargets(patch string) []string {
	seen := map[string]struct{}{}
	lines := splitLines(patch)
	add := func(value string) {
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(value, "a/")
		value = strings.TrimPrefix(value, "b/")
		if value == "" || value == "/dev/null" {
			return
		}
		value = filepath.Clean(value)
		if value == "." || strings.HasPrefix(value, "..") {
			return
		}
		seen[filepath.ToSlash(value)] = struct{}{}
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
			add(strings.TrimSpace(line[4:]))
		case strings.HasPrefix(line, "diff --git "):
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				add(fields[2])
				add(fields[3])
			}
		case strings.HasPrefix(line, "rename from "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "rename from ")))
		case strings.HasPrefix(line, "rename to "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "rename to ")))
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func classifyGitStatus(xy string) string {
	switch {
	case strings.HasPrefix(xy, "??"):
		return "untracked"
	case strings.Contains(xy, "R"):
		return "renamed"
	case strings.Contains(xy, "A"):
		return "added"
	case strings.Contains(xy, "D"):
		return "deleted"
	case strings.Contains(xy, "C"):
		return "copied"
	case strings.Contains(xy, "U"):
		return "conflicted"
	default:
		return "modified"
	}
}

func classifyDiffStatus(code string) string {
	switch {
	case strings.HasPrefix(code, "R"):
		return "renamed"
	case strings.HasPrefix(code, "A"):
		return "added"
	case strings.HasPrefix(code, "D"):
		return "deleted"
	case strings.HasPrefix(code, "C"):
		return "copied"
	default:
		return "modified"
	}
}

func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func sliceContext(lines []string, start int, end int) []string {
	if len(lines) == 0 {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	for _, line := range lines[start:end] {
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

func looksLikeText(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	return utf8.Valid(raw)
}

func detectLanguage(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "go.mod":
		return "Go"
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return "JavaScript"
	case "pyproject.toml", "requirements.txt":
		return "Python"
	case "cargo.toml":
		return "Rust"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "Go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".c", ".h":
		return "C"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "C++"
	case ".swift":
		return "Swift"
	case ".kt":
		return "Kotlin"
	case ".sh":
		return "Shell"
	case ".md":
		return "Markdown"
	case ".yaml", ".yml":
		return "YAML"
	case ".json":
		return "JSON"
	case ".toml":
		return "TOML"
	case ".css":
		return "CSS"
	case ".html":
		return "HTML"
	default:
		return ""
	}
}

func isKeyRepoFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "readme.md", "go.mod", "package.json", "pyproject.toml", "cargo.toml", "makefile", "dockerfile", "config.yaml":
		return true
	default:
		return false
	}
}

func countLinesInFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return 0
	}
	return bytes.Count(raw, []byte{'\n'}) + 1
}

func containsPath(values map[string]struct{}, path string) bool {
	if _, ok := values[path]; ok {
		return true
	}
	return false
}

func atoiDefault(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	out, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return out
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncateEngineeringText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
