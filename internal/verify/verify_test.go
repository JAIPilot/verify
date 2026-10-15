package verify

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, root, p, s string) {
	t.Helper()
	if err := writeBytes(filepath.Join(root, p), []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "verify@example.invalid"}, {"config", "user.name", "Verify test"}} {
		if _, err := git(root, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 1;\n")
	mustWrite(t, root, ".gitignore", "node_modules/\ncoverage/\nreports/\n")
	if _, err := git(root, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, nil, "commit", "-m", "baseline"); err != nil {
		t.Fatal(err)
	}
	return root
}
func setupRunner(t *testing.T, root string) *runner {
	t.Helper()
	w, err := prepare(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = git(root, nil, "worktree", "remove", "--force", w.Baseline)
		_, _ = git(root, nil, "worktree", "remove", "--force", w.Candidate)
		_ = os.RemoveAll(w.RunDir)
	})
	initial, err := snapshot(w.Baseline, w.Known)
	if err != nil {
		t.Fatal(err)
	}
	return &runner{W: w, BaselineOriginal: initial, Ask: func(string) (string, bool) { return "yes", true }}
}
func TestSnapshotIncludesCommittedStagedUnstagedAndUntracked(t *testing.T) {
	root := fixture(t)
	if _, err := git(root, nil, "checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	_, _ = git(root, nil, "commit", "-am", "feature change")
	mustWrite(t, root, "staged.cjs", "module.exports = 1;\n")
	_, _ = git(root, nil, "add", "staged.cjs")
	mustWrite(t, root, "staged.cjs", "module.exports = 2;\n")
	mustWrite(t, root, "untracked.cjs", "module.exports = 3;\n")
	before, _ := gitText(root, "status", "--porcelain")
	r := setupRunner(t, root)
	for p, want := range map[string]string{"calc.cjs": "x + 2", "staged.cjs": "= 2", "untracked.cjs": "= 3"} {
		b, err := os.ReadFile(filepath.Join(r.W.Candidate, p))
		if err != nil || !strings.Contains(string(b), want) {
			t.Fatalf("candidate %s: %s %v", p, b, err)
		}
	}
	after, _ := gitText(root, "status", "--porcelain")
	if before != after {
		t.Fatal("snapshot changed original status")
	}
	if len(r.W.BaselineScope) != 1 || len(r.W.FinalScope) != 3 {
		t.Fatalf("scopes: %v / %v", r.W.BaselineScope, r.W.FinalScope)
	}
}
func TestFrozenCharacterizationDetectsRegressionAndFinalLockPasses(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	r := setupRunner(t, root)
	ctx := context.Background()
	test := `const {test}=require('node:test'); const assert=require('node:assert/strict'); test('locked behavior',()=>assert.equal(require('./calc.cjs')(1),2));`
	if _, err := r.run(ctx, "baseline", "write_file", object{"path": "characterization.test.cjs", "content": test}); err != nil {
		t.Fatal(err)
	}
	in := object{"kind": "tests", "command": "node --test", "timeoutSeconds": 30}
	if out, err := r.run(ctx, "baseline", "measure", in); err != nil || num(out["exitCode"]) != 0 {
		t.Fatalf("baseline %v %v", out, err)
	}
	out, err := r.run(ctx, "baseline", "replay_baseline", object{})
	if err != nil {
		t.Fatal(err)
	}
	frozen := out["characterizationTests"].(CommandResult)
	if frozen.ExitCode == 0 {
		t.Fatal("characterization failed to detect regression")
	}
	original, _ := os.ReadFile(filepath.Join(root, "calc.cjs"))
	if !strings.Contains(string(original), "x + 2") {
		t.Fatal("original checkout changed")
	}
	if _, err = r.run(ctx, "review", "write_file", object{"path": "calc.cjs", "content": "module.exports = x => x + 1;\n", "explanation": "Restore intended behavior"}); err != nil {
		t.Fatal(err)
	}
	if out, err = r.run(ctx, "review", "finish_stage", object{"summary": "The behavior should remain +1"}); err != nil || out["accepted"] != true {
		t.Fatalf("accept %v %v", out, err)
	}
	if out, err = r.run(ctx, "lock", "measure", in); err != nil || num(out["exitCode"]) != 0 {
		t.Fatalf("final tests %v %v", out, err)
	}
	if err = r.W.promote(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "calc.cjs"))
	if !strings.Contains(string(b), "x + 1") {
		t.Fatal("accepted source not promoted")
	}
	if _, err = os.Stat(filepath.Join(root, "characterization.test.cjs")); err != nil {
		t.Fatal("characterization test not promoted")
	}
}
func TestEditsRequireAcceptanceAndCommandsCannotChangeSource(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	r := setupRunner(t, root)
	r.Ask = func(string) (string, bool) { return "keep +2", true }
	_, err := r.run(context.Background(), "review", "write_file", object{"path": "calc.cjs", "content": "wrong"})
	if err == nil {
		t.Fatal("rejected edit was accepted")
	}
	b, _ := os.ReadFile(filepath.Join(r.W.Candidate, "calc.cjs"))
	if string(b) == "wrong" {
		t.Fatal("rejected edit was written")
	}
	result := r.tool(context.Background(), "review", object{"id": "unsafe", "name": "run_command", "input": object{"command": "echo forbidden > calc.cjs", "timeoutSeconds": 10}})
	if obj(result["value"])["needsReview"] != true || result["isError"] != true {
		t.Fatalf("source mutation not stopped: %v", result)
	}
	b, _ = os.ReadFile(filepath.Join(root, "calc.cjs"))
	if !strings.Contains(string(b), "x + 2") {
		t.Fatal("original was changed")
	}
}
func TestConcurrentCheckoutChangesPreventPromotion(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	r := setupRunner(t, root)
	mustWrite(t, r.W.Candidate, "calc.cjs", "module.exports = x => x + 3;\n")
	mustWrite(t, root, "calc.cjs", "user edits\n")
	if err := r.W.promote(); err == nil {
		t.Fatal("overwrote concurrent edits")
	}
	b, _ := os.ReadFile(filepath.Join(root, "calc.cjs"))
	if string(b) != "user edits\n" {
		t.Fatal("concurrent edits lost")
	}
}
func TestSafePathRejectsSymlinksAndTraversal(t *testing.T) {
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
func TestNativeReportParsers(t *testing.T) {
	cases := []struct {
		format, data, path string
		covered, total     int
	}{
		{"istanbul", `{"/repo/src/a.js":{"path":"/repo/src/a.js","s":{"0":1,"1":0},"b":{"0":[1,0]}}}`, "src/a.js", 1, 2},
		{"lcov", "SF:src/a.js\nDA:1,1\nDA:2,0\nBRDA:1,0,0,1\nBRDA:1,0,1,-\nend_of_record\n", "src/a.js", 1, 2},
		{"jacoco", `<report><package name="com/acme"><sourcefile name="A.java"><counter type="LINE" missed="1" covered="1"/><counter type="BRANCH" missed="1" covered="1"/></sourcefile></package></report>`, "src/main/java/com/acme/A.java", 1, 2},
		{"cobertura", `<coverage><packages><package><classes><class filename="src/a.py"><lines><line hits="1" branch="true" condition-coverage="50% (1/2)"/><line hits="0"/></lines></class></classes></package></packages></coverage>`, "src/a.py", 1, 2},
		{"stryker", `{"files":{"src/a.js":{"mutants":[{"status":"Killed"},{"status":"Survived"},{"status":"CompileError"}]}}}`, "src/a.js", 1, 2},
		{"pit", `<mutations><mutation status="KILLED"><sourceFile>A.java</sourceFile><mutatedClass>com.acme.A</mutatedClass></mutation><mutation status="TIMED_OUT"><sourceFile>A.java</sourceFile><mutatedClass>com.acme.A</mutatedClass></mutation></mutations>`, "src/main/java/com/acme/A.java", 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			m, err := parseMetrics([]byte(tc.data), tc.format, "/repo", []string{tc.path})
			if err != nil {
				t.Fatal(err)
			}
			if m.Counters[0].Covered != tc.covered || m.Counters[0].Total != tc.total {
				t.Fatalf("wrong counters %v", m)
			}
		})
	}
}
func TestMetricsRejectMissingEmptyMalformedAndAmbiguousFiles(t *testing.T) {
	for _, data := range []string{`{}`, `{"files":{"src/a.js":{"mutants":[]}}}`, `{"files":{"src/a.js":{"mutants":[{"status":"CompileError"}]}}}`} {
		if _, err := parseMetrics([]byte(data), "stryker", "/repo", []string{"src/a.js"}); err == nil {
			t.Errorf("empty metrics passed: %s", data)
		}
	}
	if _, err := parseMetrics([]byte("SF:a.js\nDA:1,bad\nend_of_record\n"), "lcov", "/repo", []string{"a.js"}); err == nil {
		t.Fatal("malformed metrics accepted")
	}
	if _, err := scopedPath("a.js", "/repo", []string{"one/a.js", "two/a.js"}); err == nil {
		t.Fatal("ambiguous file accepted")
	}
}
func TestNonInteractiveRunFinishesWithNeedsReviewReport(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	old := requestService
	defer func() { requestService = old }()
	step := 0
	requestService = func(_ context.Context, body object) (object, error) {
		step++
		switch step {
		case 1:
			return object{"phase": "review", "state": "opaque", "content": []any{object{"id": "approval", "name": "finish_stage", "input": object{"summary": "Review required"}}}}, nil
		case 2:
			results := body["results"].([]object)
			if obj(results[0]["value"])["needsReview"] != true {
				t.Fatal("noninteractive approval bypassed")
			}
			return object{"phase": "report", "state": "opaque2", "content": []any{object{"id": "report", "name": "finish_report", "input": object{"report": "Review needed"}}}}, nil
		default:
			return object{"phase": "done", "verdict": "needs_review", "report": "Review needed"}, nil
		}
	}
	report, err := verify(context.Background(), options{Repo: root, Base: "main", NonInteractive: true, MaxTurns: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report["verdict"] != "needs_review" || report["applied"] != false {
		t.Fatalf("bad report: %v", report)
	}
	b, err := os.ReadFile(str(report["reportPath"]))
	if err != nil {
		t.Fatal(err)
	}
	var parsed object
	if json.Unmarshal(b, &parsed) != nil || parsed["verdict"] != "needs_review" {
		t.Fatal("report not persisted")
	}
	workspaces := obj(report["workspaces"])
	for _, key := range []string{"baseline", "candidate"} {
		_, _ = git(root, nil, "worktree", "remove", "--force", str(workspaces[key]))
	}
	_ = os.RemoveAll(filepath.Dir(str(report["reportPath"])))
}
func TestProposedEditDisplaysTheActualDiff(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = write
	printFileDiff("sample.txt", []byte("before\n"), []byte("after\n"), false)
	_ = write.Close()
	os.Stderr = previous
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "-before") || !strings.Contains(string(output), "+after") {
		t.Fatalf("approval preview omitted the edit: %s", output)
	}
}
func TestGroupedReadsAndUsageAreRecorded(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	old := requestService
	defer func() { requestService = old }()
	step := 0
	requestService = func(_ context.Context, body object) (object, error) {
		step++
		if step == 1 {
			return object{
				"phase": "baseline", "state": "opaque",
				"usage": object{"inputTokens": 100, "outputTokens": 20, "cacheReadTokens": 50, "cacheWriteTokens": 10, "providerUsd": "0.001000000"},
				"content": []any{
					object{"id": "a", "name": "read_file", "input": object{"path": "calc.cjs"}},
					object{"id": "b", "name": "read_file", "input": object{"path": ".gitignore"}},
				},
			}, nil
		}
		results := body["results"].([]object)
		if len(results) != 2 || !strings.Contains(str(obj(results[0]["value"])["content"]), "x + 1") || !strings.Contains(str(obj(results[1]["value"])["content"]), "node_modules") {
			t.Fatalf("grouped reads returned out of order: %v", results)
		}
		return object{"phase": "done", "verdict": "needs_review", "report": "Read complete"}, nil
	}
	report, err := verify(context.Background(), options{Repo: root, Base: "main", NonInteractive: true, MaxTurns: 10})
	if err != nil {
		t.Fatal(err)
	}
	usage := obj(report["usage"])
	if num(usage["modelTurns"]) != 1 || num(usage["cacheReadTokens"]) != 50 || usage["estimatedProviderUsd"] != 0.001 {
		t.Fatalf("usage was not recorded: %v", usage)
	}
	workspaces := obj(report["workspaces"])
	for _, key := range []string{"baseline", "candidate"} {
		_, _ = git(root, nil, "worktree", "remove", "--force", str(workspaces[key]))
	}
	_ = os.RemoveAll(filepath.Dir(str(report["reportPath"])))
}
func TestTextOnlyAgentTurnContinues(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	old := requestService
	defer func() { requestService = old }()
	step := 0
	requestService = func(_ context.Context, body object) (object, error) {
		step++
		if step == 1 {
			return object{"phase": "baseline", "state": "opaque", "content": []any{}}, nil
		}
		if results, ok := body["results"].([]object); !ok || len(results) != 0 {
			t.Fatalf("empty tool turn was not acknowledged: %v", body["results"])
		}
		return object{"phase": "done", "verdict": "not_ready", "report": "Agent stopped"}, nil
	}
	report, err := verify(context.Background(), options{Repo: root, Base: "main", NonInteractive: true, MaxTurns: 10})
	if err != nil || step != 2 {
		t.Fatalf("text-only turn stopped the run: %v, steps %d", err, step)
	}
	workspaces := obj(report["workspaces"])
	for _, key := range []string{"baseline", "candidate"} {
		_, _ = git(root, nil, "worktree", "remove", "--force", str(workspaces[key]))
	}
	_ = os.RemoveAll(filepath.Dir(str(report["reportPath"])))
}
func TestMixedToolGroupRunsInOrder(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	old := requestService
	defer func() { requestService = old }()
	step := 0
	requestService = func(_ context.Context, body object) (object, error) {
		step++
		if step == 1 {
			return object{"phase": "baseline", "state": "opaque", "content": []any{
				object{"id": "write", "name": "run_command", "input": object{"command": "printf staged > GroupedTests.java", "purpose": "Create test", "timeoutSeconds": 5}},
				object{"id": "read", "name": "read_file", "input": object{"path": "GroupedTests.java"}},
			}}, nil
		}
		results := body["results"].([]object)
		if len(results) != 2 || str(obj(results[1]["value"])["content"]) != "staged" {
			t.Fatalf("mixed tools ran out of order: %v", results)
		}
		return object{"phase": "done", "verdict": "not_ready", "report": "Inspection complete"}, nil
	}
	report, err := verify(context.Background(), options{Repo: root, Base: "main", NonInteractive: true, MaxTurns: 10})
	if err != nil || step != 2 {
		t.Fatalf("mixed group failed: %v, steps %d", err, step)
	}
	workspaces := obj(report["workspaces"])
	for _, key := range []string{"baseline", "candidate"} {
		_, _ = git(root, nil, "worktree", "remove", "--force", str(workspaces[key]))
	}
	_ = os.RemoveAll(filepath.Dir(str(report["reportPath"])))
}
