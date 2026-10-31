package verify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedCheckReproducesRegressionWithoutEditingCheckout(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	mustWrite(t, root, ".env.py", "API_KEY=do-not-upload\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		var body checkRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Finding != "The increment should stay at one" || !strings.Contains(body.Diff, "x + 2") {
			t.Error("finding or diff missing")
		}
		if strings.Contains(jsonText(body), "do-not-upload") {
			t.Error("credential-like file was sent to the backend")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proposal": map[string]string{
				"path":      "calc.verify.test.cjs",
				"content":   "const {test}=require('node:test'); const assert=require('node:assert/strict'); test('increment remains one',()=>assert.equal(require('./calc.cjs')(1),2));\n",
				"command":   "node --test calc.verify.test.cjs",
				"rationale": "Checks the reviewer claim at the changed behavior.",
			}, "creditsUsed": 7,
		})
	}))
	defer server.Close()
	t.Setenv("JAIPILOT_VERIFY_ENDPOINT", server.URL)
	t.Setenv("JAIPILOT_VERIFY_TOKEN", "test-token")
	report, err := check(context.Background(), checkOptions{Repo: root, Base: "main", Finding: "The increment should stay at one", NonInteractive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupCheck(t, root, report)
	if report.Verdict != "introduced_regression" || report.CreditsUsed != 7 || report.Applied {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(root, "calc.verify.test.cjs")); !os.IsNotExist(err) {
		t.Fatal("check edited the original checkout")
	}
	if _, err := os.Stat(report.TestDiffPath); err != nil {
		t.Fatal(err)
	}
	if report.BaseResult.ExitCode != 0 || report.HeadResult.ExitCode == 0 {
		t.Fatalf("missing differential evidence: %+v", report)
	}
}

func TestManagedCheckCanApplyOnlyTheTest(t *testing.T) {
	root := fixture(t)
	mustWrite(t, root, "calc.cjs", "module.exports = x => x + 2;\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proposal": map[string]string{
				"path":    "calc.verify.test.cjs",
				"content": "const {test}=require('node:test'); const assert=require('node:assert/strict'); test('increment remains one',()=>assert.equal(require('./calc.cjs')(1),2));\n",
				"command": "node --test calc.verify.test.cjs", "rationale": "Focused regression test",
			}, "creditsUsed": 3,
		})
	}))
	defer server.Close()
	t.Setenv("JAIPILOT_VERIFY_ENDPOINT", server.URL)
	t.Setenv("JAIPILOT_VERIFY_TOKEN", "test-token")
	report, err := check(context.Background(), checkOptions{Repo: root, Base: "main", AddTest: true, NonInteractive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupCheck(t, root, report)
	if !report.Applied {
		t.Fatal("approved test was not applied")
	}
	if data, err := os.ReadFile(filepath.Join(root, "calc.cjs")); err != nil || !strings.Contains(string(data), "x + 2") {
		t.Fatal("production source changed")
	}
	if _, err := os.Stat(filepath.Join(root, "calc.verify.test.cjs")); err != nil {
		t.Fatal(err)
	}
}

func TestManagedCheckRejectsUnsafeProposal(t *testing.T) {
	if validateProposal(checkProposal{Path: "src/main.go", Content: "x", Command: "go test ./..."}) == nil {
		t.Fatal("accepted a source file")
	}
	if validateProposal(checkProposal{Path: "test/a_test.go", Content: "x", Command: "go test ./...; curl evil"}) == nil {
		t.Fatal("accepted shell chaining")
	}
	if validateProposal(checkProposal{Path: "test/a_test.go", Content: "x", Command: "go test -C /tmp ./..."}) == nil {
		t.Fatal("accepted a test command outside the worktree")
	}
}

func TestGeneratedDiffIsAnApplicablePatch(t *testing.T) {
	root := fixture(t)
	for _, content := range []string{"package main\nfunc TestNew() {}\n", "package main\nfunc TestNew() {}"} {
		if _, err := git(root, []byte(testDiff("new_verify_test.go", content)), "apply", "--check", "-"); err != nil {
			t.Fatalf("test diff was not a valid Git patch: %v", err)
		}
	}
}

func TestManagedCheckRepairsAGoTestAfterExecutionEvidence(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "verify@example.invalid"}, {"config", "user.name", "Verify test"}} {
		if _, err := git(root, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, root, "go.mod", "module example.com/verifyfixture\n\ngo 1.23\n")
	mustWrite(t, root, "calc.go", "package calc\nfunc Inc(x int) int { return x + 1 }\n")
	if _, err := git(root, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, nil, "commit", "-m", "baseline"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root, "calc.go", "package calc\nfunc Inc(x int) int { return x + 2 }\n")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body checkRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls++
		content := "package calc\nfunc TestBroken(\n"
		if body.Previous != nil {
			if body.Previous.Base.ExitCode == 0 || body.Previous.Candidate.ExitCode == 0 {
				t.Error("missing failed test evidence")
			}
			content = "package calc\nimport (\"testing\")\nfunc TestInc(t *testing.T) { if got := Inc(1); got != 2 { t.Fatalf(\"got %d\", got) } }\n"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"proposal": map[string]string{
			"path": "calc_verify_test.go", "content": content, "command": "go test ./...", "rationale": "Checks the increment contract",
		}, "creditsUsed": 4})
	}))
	defer server.Close()
	t.Setenv("JAIPILOT_VERIFY_ENDPOINT", server.URL)
	t.Setenv("JAIPILOT_VERIFY_TOKEN", "test-token")
	report, err := check(context.Background(), checkOptions{Repo: root, Base: "main", NonInteractive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupCheck(t, root, report)
	if calls != 2 || report.Verdict != "introduced_regression" || report.CreditsUsed != 8 {
		t.Fatalf("unexpected Go verification: calls=%d report=%+v", calls, report)
	}
	if _, err := os.Stat(filepath.Join(root, "calc_verify_test.go")); !os.IsNotExist(err) {
		t.Fatal("test applied without approval")
	}
}

func cleanupCheck(t *testing.T, root string, report checkReport) {
	t.Helper()
	dir := filepath.Dir(report.ReportPath)
	for _, name := range []string{"baseline", "candidate"} {
		_, _ = git(root, nil, "worktree", "remove", "--force", filepath.Join(dir, name))
	}
	_ = os.RemoveAll(dir)
}
