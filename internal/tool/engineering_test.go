package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/policy"
)

func TestRepoIndexToolBuildsStructuredInventory(t *testing.T) {
	repo := initTestRepo(t, map[string]string{
		"README.md":        "# Demo\n",
		"go.mod":           "module example.com/demo\n\ngo 1.23\n",
		"cmd/demo/main.go": "package main\n\nfunc main() {}\n",
		"web/app.ts":       "export const answer = 42;\n",
	})

	out, err := invokeTool(t, NewRepoIndexTool(), context.Background(), map[string]any{
		"path":      repo,
		"max_files": 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["is_git_repo"] != true {
		t.Fatalf("expected git repo in index, got %#v", payload["is_git_repo"])
	}
	if payload["file_count"].(float64) < 4 {
		t.Fatalf("expected indexed files, got %#v", payload["file_count"])
	}
	languages, ok := payload["languages"].([]any)
	if !ok || len(languages) == 0 {
		t.Fatalf("expected language inventory, got %#v", payload["languages"])
	}
	keyFiles, ok := payload["key_files"].([]any)
	if !ok || len(keyFiles) == 0 {
		t.Fatalf("expected key files, got %#v", payload["key_files"])
	}
}

func TestRepoSearchToolReturnsStructuredContentMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\n// TODO: tighten search\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := invokeTool(t, NewRepoSearchTool(config.ToolsConfig{MaxCommandBytes: 16 * 1024}), context.Background(), map[string]any{
		"path":          root,
		"query":         "TODO",
		"mode":          "content",
		"context_lines": 1,
		"limit":         10,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("expected structured results, got %#v", payload["results"])
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first result map, got %#v", results[0])
	}
	if first["path"] != "main.go" {
		t.Fatalf("expected main.go result, got %#v", first["path"])
	}
	if first["line"] != float64(3) {
		t.Fatalf("expected line 3 match, got %#v", first["line"])
	}
}

func TestApplyDiffToolAppliesPatchAndReportsSummary(t *testing.T) {
	repo := initTestRepo(t, map[string]string{
		"file.txt": "hello\nworld\n",
	})
	tool := NewApplyDiffTool(config.ToolsConfig{MaxCommandBytes: 16 * 1024})
	patch := strings.TrimSpace(`
diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
-hello
+hi
 world
diff --git a/notes.md b/notes.md
new file mode 100644
--- /dev/null
+++ b/notes.md
@@ -0,0 +1 @@
+# Notes
`) + "\n"

	out, err := invokeTool(t, tool, context.Background(), map[string]any{
		"path":  repo,
		"patch": patch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "file.txt")); err != nil || string(raw) != "hi\nworld\n" {
		t.Fatalf("expected patched file content, got %q err=%v", string(raw), err)
	}
	if _, err := os.Stat(filepath.Join(repo, "notes.md")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"change_summary"`) {
		t.Fatalf("expected change summary in output, got %s", out)
	}
}

func TestChangeSummaryToolReportsModifiedAndUntrackedFiles(t *testing.T) {
	repo := initTestRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := invokeTool(t, NewChangeSummaryTool(config.ToolsConfig{MaxCommandBytes: 16 * 1024}), context.Background(), map[string]any{
		"path": repo,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["files_changed"].(float64) < 2 {
		t.Fatalf("expected at least two changed files, got %#v", payload["files_changed"])
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) < 2 {
		t.Fatalf("expected structured file summaries, got %#v", payload["files"])
	}
	var sawModified, sawUntracked bool
	for _, item := range files {
		file, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch file["status"] {
		case "modified":
			sawModified = true
		case "untracked":
			sawUntracked = true
		}
	}
	if !sawModified || !sawUntracked {
		t.Fatalf("expected modified and untracked files, got %s", out)
	}
}

func TestTestFailureLocatorToolRunsCommandAndFindsSuspect(t *testing.T) {
	workdir := t.TempDir()
	testFile := filepath.Join(workdir, "sample_test.go")
	if err := os.WriteFile(testFile, []byte("package demo\n\nfunc TestSample(t *testing.T) {\n\tif false {\n\t\tpanic(\"x\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.ToolsConfig{
		AllowCommandExecution: true,
		CommandShell:          "bash",
		MaxCommandBytes:       16 * 1024,
	}
	tool := NewTestFailureLocatorTool(cfg, policy.NewEngine(cfg))
	command := "printf '%s\n' '--- FAIL: TestSample (0.00s)' 'sample_test.go:5: expected true, got false'; exit 1"

	out, err := invokeTool(t, tool, context.Background(), map[string]any{
		"command":       command,
		"workdir":       workdir,
		"context_lines": 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["framework"] != "go test" {
		t.Fatalf("expected go test framework, got %#v", payload["framework"])
	}
	if payload["exit_code"] != float64(1) {
		t.Fatalf("expected exit code 1, got %#v", payload["exit_code"])
	}
	primary, ok := payload["primary_suspect"].(map[string]any)
	if !ok {
		t.Fatalf("expected primary suspect, got %#v", payload["primary_suspect"])
	}
	if primary["path"] != "sample_test.go" {
		t.Fatalf("expected sample_test.go suspect, got %#v", primary["path"])
	}
	if primary["line"] != float64(5) {
		t.Fatalf("expected localized line 5, got %#v", primary["line"])
	}
}

func initTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runInDir(t, root, "git", "init", "-q")
	runInDir(t, root, "git", "add", ".")
	runInDir(t, root, "git", "-c", "user.name=Qorvexus Test", "-c", "user.email=test@example.com", "commit", "-qm", "init")
	return root
}

func runInDir(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, string(out))
	}
}
