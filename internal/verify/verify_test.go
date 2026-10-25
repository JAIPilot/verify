package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "verify@example.invalid"}, {"config", "user.name", "Verify test"}} {
		if _, err := git(root, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 1;\n")
	mustWrite(t, root, "existing.test.cjs", "const {test}=require('node:test'); const assert=require('node:assert/strict'); test('existing suite',()=>assert.equal(typeof require('./calc.cjs'),'function'));\n")
	if _, err := git(root, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, nil, "commit", "-m", "baseline"); err != nil {
		t.Fatal(err)
	}
	return root
}

func mustWrite(t *testing.T, root, name, content string) {
	t.Helper()
	if err := writeBytes(filepath.Join(root, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func cleanupRun(t *testing.T, root string, report object) {
	t.Helper()
	workspaces := obj(report["workspaces"])
	for _, name := range []string{"baseline", "candidate"} {
		if path := str(workspaces[name]); path != "" {
			_, _ = git(root, nil, "worktree", "remove", "--force", path)
		}
	}
	if path := str(report["reportPath"]); path != "" {
		_ = os.RemoveAll(filepath.Dir(path))
	}
}

func TestLocalPromptWorkflowFindsRegressionAndAppliesApprovedDiff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake coding-agent executable is a POSIX shell script")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for this integration fixture")
	}
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	fakeBin := t.TempDir()
	script := `#!/bin/sh
prompt=$(cat)
result=$(printf '%s\n' "$prompt" | sed -n 's/^Write your result as a single JSON object to \([^ ]*\) in this workspace.*/\1/p')
if [ -z "$result" ]; then exit 10; fi
case "$prompt" in
  *"STAGE 1: BASELINE."*)
    cat > calc.test.cjs <<'EOF'
const {test}=require('node:test'); const assert=require('node:assert/strict'); test('old behavior',()=>assert.equal(require('./calc.cjs')(1),2));
EOF
    printf '%s' '{"summary":"Locked the old plus-one behavior.","testCommand":"node --test calc.test.cjs","existingSuiteCommand":"node --test"}' > "$result"
    ;;
  *"STAGE 2: REVIEW."*)
    case "$prompt" in
      *'"answer":"Preserve old"'*)
        printf '%s\n' 'module.exports = x => x + 1;' > calc.cjs
        printf '%s' '{"summary":"Restored old behavior after the user chose to preserve it."}' > "$result"
        ;;
      *)
        printf '%s' '{"question":"Keep the old plus-one result?","options":["Preserve old","Accept new"]}' > "$result"
        ;;
    esac
    ;;
  *"STAGE 3: TEST THE ACCEPTED CHANGE."*)
    cat > new.test.cjs <<'EOF'
const {test}=require('node:test'); const assert=require('node:assert/strict'); test('accepted behavior',()=>assert.equal(require('./calc.cjs')(2),3));
EOF
    printf '%s' '{"summary":"Added a test for the accepted behavior.","testCommand":"node --test new.test.cjs"}' > "$result"
    ;;
  *) exit 11 ;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input := filepath.Join(t.TempDir(), "answers")
	if err := os.WriteFile(input, []byte("1\n1\n1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	oldInput := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = oldInput }()
	report, err := verify(context.Background(), options{Repo: root, Base: "main", Agent: "codex"})
	if report != nil {
		defer cleanupRun(t, root, report)
	}
	if err != nil {
		t.Fatalf("verify: %v, report=%v", err, report)
	}
	if report["verdict"] != "good_to_go" || report["applied"] != true {
		t.Fatalf("unexpected verdict: %v", report)
	}
	if report["replay"].(CommandResult).ExitCode == 0 {
		t.Fatal("old behavior replay should fail on the original candidate")
	}
	if report["finalTests"].(CommandResult).Command != "node --test" {
		t.Fatal("final suite must use the original full-suite command")
	}
	if data, err := os.ReadFile(filepath.Join(root, "calc.cjs")); err != nil || !strings.Contains(string(data), "x + 1") {
		t.Fatalf("approved change was not applied: %s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.test.cjs")); err != nil {
		t.Fatal("final test was not applied")
	}
}

func TestSelectAgentRequiresInstalledCLI(t *testing.T) {
	if _, err := selectAgent("unknown"); err == nil {
		t.Fatal("accepted unknown agent")
	}
	if _, err := selectAgent("claude"); err == nil {
		if _, lookupErr := exec.LookPath("claude"); lookupErr != nil {
			t.Fatal("selected missing Claude CLI")
		}
	}
}

func TestClaudeAdapterUsesLocalStageResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Claude executable is a POSIX shell script")
	}
	root := t.TempDir()
	bin := t.TempDir()
	script := `#!/bin/sh
for argument in "$@"; do prompt=$argument; done
result=$(printf '%s\n' "$prompt" | sed -n 's/^Write your result as a single JSON object to \([^ ]*\) in this workspace.*/\1/p')
if [ -z "$result" ]; then exit 10; fi
printf '%s' '{"summary":"Local Claude stage completed."}' > "$result"
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	result, err := runAgentStage(context.Background(), "claude", root, t.TempDir(), "smoke", "Read this fixture.")
	if err != nil || result.Summary != "Local Claude stage completed." {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBareVerifyShowsHelpAndRequiresStart(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "cli-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = output, output
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	if code := Main(nil); code != 0 {
		t.Fatalf("bare verify returned %d", code)
	}
	if code := Main([]string{"--agent", "codex"}); code != 2 {
		t.Fatalf("verification without start returned %d", code)
	}
	if code := Main([]string{"start", "--help"}); code != 0 {
		t.Fatalf("start help returned %d", code)
	}
	if code := Main([]string{"start", "--version"}); code != 0 {
		t.Fatalf("start version returned %d", code)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Usage: verify start") || !strings.Contains(string(data), "Unknown command: --agent") {
		t.Fatalf("unexpected help and command output: %s", data)
	}
}

func TestAgentTranscriptStaysOutOfTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake coding-agent executable is a POSIX shell script")
	}
	root := t.TempDir()
	bin := t.TempDir()
	script := `#!/bin/sh
prompt=$(cat)
result=$(printf '%s\n' "$prompt" | sed -n 's/^Write your result as a single JSON object to \([^ ]*\) in this workspace.*/\1/p')
printf 'AGENT_IS_WORKING\n'
printf '%s' '{"summary":"Completed after writing tests."}' > "$result"
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := os.CreateTemp(t.TempDir(), "agent-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	stderr := os.Stderr
	os.Stderr = output
	defer func() { os.Stderr = stderr }()
	runDir := t.TempDir()
	if _, err := runAgentStage(context.Background(), "codex", root, runDir, "smoke", "Read this fixture."); err != nil {
		t.Fatal(err)
	}
	shown, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(shown), "AGENT_IS_WORKING") || strings.Contains(string(shown), "Read this fixture") ||
		strings.Contains(string(shown), "Agent log:") || !strings.Contains(string(shown), "working on smoke") {
		t.Fatalf("terminal showed a transcript or hid progress: %s", shown)
	}
	log, err := os.ReadFile(filepath.Join(runDir, "smoke.log"))
	if err != nil || !strings.Contains(string(log), "AGENT_IS_WORKING") {
		t.Fatalf("agent log did not retain output: %s %v", log, err)
	}
}

func TestEvidenceCommandReportsStatusWithoutRawOutput(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "command-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	stderr := os.Stderr
	os.Stderr = output
	defer func() { os.Stderr = stderr }()
	result, err := runCommand(context.Background(), t.TempDir(), object{
		"command": "printf TEST_IS_VISIBLE", "timeoutSeconds": 5, "purpose": "test",
	})
	if err != nil || result.ExitCode != 0 || !strings.Contains(result.Output, "TEST_IS_VISIBLE") {
		t.Fatalf("command result=%+v err=%v", result, err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil || strings.Contains(string(data), "TEST_IS_VISIBLE") || !strings.Contains(string(data), "Passed") {
		t.Fatalf("test status was missing or raw output was visible: %s %v", data, err)
	}
}

func TestEvidenceRejectsChainedCommands(t *testing.T) {
	for _, command := range []string{"false | true", "false; true", "false && true", "false > out", ""} {
		if err := directEvidenceCommand(command); err == nil {
			t.Errorf("accepted %q", command)
		}
	}
}

func TestEmbeddedPromptsRenderContext(t *testing.T) {
	w := &workspace{Base: "abc123", BaselineScope: []string{"src/a.java"}, FinalScope: []string{"src/a.java"}}
	baseline := promptBaseline(w)
	if !strings.Contains(baseline, "abc123") || !strings.Contains(baseline, "src/a.java") || strings.Contains(baseline, "{{") ||
		!strings.Contains(baseline, "Do not run build, test, or coverage commands") {
		t.Fatalf("baseline prompt was not rendered: %q", baseline)
	}
	review := promptReview(w, object{"replay": CommandResult{Command: "mvn test", ExitCode: 1, Output: "failed"}}, []object{{"answer": "Keep old behavior"}})
	if !strings.Contains(review, "Keep old behavior") || !strings.Contains(review, "failed") || strings.Contains(review, "{{") {
		t.Fatalf("review prompt was not rendered: %q", review)
	}
	lock := promptLock(w, object{"decisions": []object{{"answer": "Keep old behavior"}}})
	if !strings.Contains(lock, "src/a.java") || !strings.Contains(lock, "Keep old behavior") || strings.Contains(lock, "{{") {
		t.Fatalf("lock prompt was not rendered: %q", lock)
	}
}

func TestConcurrentCheckoutChangesPreventPromotion(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	w, err := prepare(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupRun(t, root, object{"workspaces": object{"baseline": w.Baseline, "candidate": w.Candidate}, "reportPath": filepath.Join(w.RunDir, "report.json")})
	mustWrite(t, w.Candidate, "calc.cjs", "module.exports = x => x + 3;\n")
	mustWrite(t, root, "calc.cjs", "user edits\n")
	if err := w.promote(); err == nil {
		t.Fatal("overwrote concurrent edits")
	}
	data, _ := os.ReadFile(filepath.Join(root, "calc.cjs"))
	if string(data) != "user edits\n" {
		t.Fatal("concurrent edits lost")
	}
}

func TestSafePathRejectsTraversalAndSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../escape", ".git/config", "a/../../escape", root} {
		if _, err := safePath(root, p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err == nil {
		if _, err = safePath(root, "link/file"); err == nil {
			t.Fatal("followed symlink")
		}
	}
}

func TestNativeCoverageReportParsers(t *testing.T) {
	cases := []struct {
		format, data, path string
	}{
		{"istanbul", `{"/repo/src/a.js":{"path":"/repo/src/a.js","s":{"0":1,"1":0},"b":{"0":[1,0]}}}`, "src/a.js"},
		{"lcov", "SF:src/a.js\nDA:1,1\nDA:2,0\nBRDA:1,0,0,1\nBRDA:1,0,1,-\nend_of_record\n", "src/a.js"},
		{"jacoco", `<report><package name="com/acme"><sourcefile name="A.java"><counter type="LINE" missed="1" covered="1"/><counter type="BRANCH" missed="1" covered="1"/></sourcefile></package></report>`, "src/main/java/com/acme/A.java"},
		{"cobertura", `<coverage><packages><package><classes><class filename="src/a.py"><lines><line hits="1" branch="true" condition-coverage="50% (1/2)"/><line hits="0"/></lines></class></classes></package></packages></coverage>`, "src/a.py"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			m, err := parseMetrics([]byte(tc.data), tc.format, "/repo", []string{tc.path})
			if err != nil {
				t.Fatal(err)
			}
			if m.Counters[0].Covered != 1 || m.Counters[0].Total != 2 {
				t.Fatalf("wrong counters: %+v", m)
			}
		})
	}
	for _, data := range []string{`{}`, `{"src/a.js":{"path":"src/a.js","s":{},"b":{}}}`} {
		if _, err := parseMetrics([]byte(data), "istanbul", "/repo", []string{"src/a.js"}); err == nil {
			t.Errorf("empty metrics passed: %s", data)
		}
	}
}
