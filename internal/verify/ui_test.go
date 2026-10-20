package verify

import (
	"bytes"
	"strings"
	"testing"
)

func TestQuestionChoicesAndComment(t *testing.T) {
	var output bytes.Buffer
	u := newTerminalUI(strings.NewReader("8\n\nKeep the existing behavior\n"), &output)
	answer, ok := u.choose("Which behavior is intended?", []string{"Preserve old", "Use new"}, true)
	if !ok || answer != "Keep the existing behavior" {
		t.Fatalf("comment answer = %q, %v", answer, ok)
	}
	for _, want := range []string{"1.  Preserve old", "2.  Use new", "Choose 1–2 or type a comment", "Choose a listed number or type a comment", "Comment sent"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q from menu: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "Add a comment") || strings.Contains(output.String(), "Your comment:") {
		t.Fatal("comment requires an unnecessary second step")
	}
	output.Reset()
	u = newTerminalUI(strings.NewReader("2\n"), &output)
	if answer, ok = u.choose("Intended?", []string{"Preserve old", "Use new"}, true); !ok || answer != "Use new" {
		t.Fatalf("selected answer = %q, %v", answer, ok)
	}
	u = newTerminalUI(strings.NewReader(""), &output)
	if _, ok = u.choose("Intended?", []string{"Yes", "No"}, true); ok {
		t.Fatal("EOF accepted an answer")
	}
	for _, tc := range []struct{ input, want string }{{"y\n", "Yes"}, {"yes\n", "Yes"}, {"N\n", "No"}, {"NO\n", "No"}} {
		output.Reset()
		u = newTerminalUI(strings.NewReader(tc.input), &output)
		if answer, ok = u.choose("Keep the change?", []string{"Yes", "No"}, true); !ok || answer != tc.want {
			t.Fatalf("binary answer %q = %q, %v", tc.input, answer, ok)
		}
		if !strings.Contains(output.String(), "(y/n)") {
			t.Fatal("binary shortcut was not shown")
		}
	}
	output.Reset()
	u = newTerminalUI(strings.NewReader("Please revise\n2\n"), &output)
	if answer, ok = u.choose("Apply?", []string{"Apply", "Leave"}, false); !ok || answer != "Leave" {
		t.Fatalf("final decision accepted free text: %q, %v", answer, ok)
	}
}

func TestReviewAndReportFormatting(t *testing.T) {
	var output bytes.Buffer
	u := newTerminalUI(strings.NewReader(""), &output)
	u.color = true
	u.stage("2", "Review intent and edits")
	u.preview("Proposed edit", "Fix the result", "@@ -1 +1 @@\n-old\n+new\n")
	u.report("# Verify: good to go\n\n## Evidence\n- **Tests** passed with `go test`\n\n| Check | Result |\n| --- | --- |\n| Tests | ✓ Pass |\n")
	got := output.String()
	for _, want := range []string{"\x1b[1;36mVERIFY", "\x1b[31m-old", "\x1b[32m+new", "\x1b[1mTests", "\x1b[36mgo test", "┌", "\x1b[32m✓ Pass"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing styled output %q", want)
		}
	}
	output.Reset()
	u.color = false
	u.preview("Diff", "", "-old\n+new\n")
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatal("plain output contains ANSI escapes")
	}
	output.Reset()
	u.report("## Report\n\n**Verdict:** `good_to_go`\n\n| Metric | Command | Result |\n| --- | --- | --- |\n| Coverage | `./mvnw -q -Dtest=org.springframework.samples.petclinic.model.NamedEntityTest test jacoco:report -Djacoco.includes=org/springframework/samples/petclinic/model/NamedEntity.class` | 12/12 lines covered |")
	if got := output.String(); !strings.Contains(got, "VERDICT  good to go") || !strings.Contains(got, "Command:") || strings.Contains(got, "┌") {
		t.Fatalf("long table was not rendered for terminal width: %s", got)
	}
}

func TestReviewMenusMapChoicesToClearFeedback(t *testing.T) {
	r := &runner{}
	var choices []string
	var withComment bool
	r.AskChoice = func(_ string, offered []string, comment bool) (string, bool) {
		choices, withComment = offered, comment
		return offered[1], true
	}
	answer, ok := r.approveEdit()
	if !ok || !withComment || len(choices) != 3 || choices[2] != "Skip this edit" || !strings.Contains(answer, "different edit") {
		t.Fatalf("edit decision: %q, %v, %v", answer, ok, choices)
	}
	answer, ok = r.approveReview()
	if !ok || !withComment || len(choices) != 2 || !strings.Contains(answer, "revise the candidate") {
		t.Fatalf("review decision: %q, %v, %v", answer, ok, choices)
	}
	r.AskChoice = func(_ string, offered []string, _ bool) (string, bool) { return offered[0], true }
	if answer, ok = r.approveEdit(); !ok || answer != "yes" {
		t.Fatalf("acceptance: %q, %v", answer, ok)
	}
	if answer, ok = r.approveReview(); !ok || answer != "yes" {
		t.Fatalf("review acceptance: %q, %v", answer, ok)
	}
}
