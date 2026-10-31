package verify

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type checkOptions struct {
	Repo, Base, Finding, FindingFile, Report string
	JSON, AddTest, NonInteractive            bool
}
type checkFile struct {
	Path      string `json:"path"`
	Base      string `json:"base,omitempty"`
	Candidate string `json:"candidate"`
}
type checkRequest struct {
	RequestID string        `json:"requestId"`
	RunID     string        `json:"runId"`
	Base      string        `json:"base"`
	Diff      string        `json:"diff"`
	Finding   string        `json:"finding,omitempty"`
	Feedback  string        `json:"feedback,omitempty"`
	Files     []checkFile   `json:"files"`
	Previous  *checkAttempt `json:"previous,omitempty"`
}
type checkProposal struct {
	Path, Content, Command, Rationale string
}
type checkAttempt struct {
	Path      string        `json:"path"`
	Content   string        `json:"content"`
	Command   string        `json:"command"`
	Base      CommandResult `json:"base"`
	Candidate CommandResult `json:"candidate"`
}
type checkReport struct {
	Verdict      string         `json:"verdict"`
	Summary      string         `json:"summary"`
	Finding      string         `json:"finding,omitempty"`
	Base         string         `json:"base"`
	TestPath     string         `json:"testPath,omitempty"`
	TestCommand  string         `json:"testCommand,omitempty"`
	Rationale    string         `json:"rationale,omitempty"`
	BaseResult   *CommandResult `json:"baseResult,omitempty"`
	HeadResult   *CommandResult `json:"headResult,omitempty"`
	CreditsUsed  int            `json:"creditsUsed"`
	Applied      bool           `json:"applied"`
	ReportPath   string         `json:"reportPath"`
	TestDiffPath string         `json:"testDiffPath,omitempty"`
}

func checkMain(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	o := checkOptions{}
	flags.StringVar(&o.Repo, "repo", ".", "Git repository containing the change")
	flags.StringVar(&o.Base, "base", "", "Base branch (auto detected by default)")
	flags.StringVar(&o.Finding, "finding", "", "Reviewer finding to investigate")
	flags.StringVar(&o.FindingFile, "finding-file", "", "Read a reviewer finding from a file")
	flags.StringVar(&o.Report, "report", "", "Also save the JSON report at this path")
	flags.BoolVar(&o.JSON, "json", false, "Print the report as JSON")
	flags.BoolVar(&o.AddTest, "add-test", false, "Add the generated test to the current checkout after verification")
	flags.BoolVar(&o.NonInteractive, "non-interactive", false, "Do not ask to add the test")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: verify [options]\n       verify check [options]\n       verify start [options]\n       verify update\n       verify --version\n\nVerify a Git change with a focused test on the base and changed revisions.\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (o.Finding != "" && o.FindingFile != "") {
		fmt.Fprintln(os.Stderr, "Use either --finding or --finding-file, with no positional arguments")
		return 2
	}
	if o.FindingFile != "" {
		b, err := os.ReadFile(o.FindingFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		o.Finding = string(b)
	}
	report, err := check(ctx, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Verify:", err)
		if report.ReportPath != "" {
			fmt.Fprintf(os.Stderr, "Credits used: %d  Report: %s\n", report.CreditsUsed, report.ReportPath)
		}
		return 1
	}
	if o.JSON {
		fmt.Println(jsonText(report))
	} else {
		fmt.Fprintf(os.Stderr, "\nVerify: %s\n%s\n", report.Verdict, report.Summary)
		if report.TestPath != "" {
			fmt.Fprintf(os.Stderr, "Test: %s  Command: %s\n", report.TestPath, report.TestCommand)
			fmt.Fprintf(os.Stderr, "Base: exit %d  Change: exit %d\n", report.BaseResult.ExitCode, report.HeadResult.ExitCode)
		}
		fmt.Fprintf(os.Stderr, "Credits: %d  Evidence: %s\n", report.CreditsUsed, report.ReportPath)
	}
	return 0
}

func check(ctx context.Context, o checkOptions) (result checkReport, err error) {
	w, err := prepare(o.Repo, o.Base)
	if err != nil {
		return result, err
	}
	result = checkReport{Verdict: "inconclusive", Finding: o.Finding, Base: w.Base, ReportPath: filepath.Join(w.RunDir, "report.json")}
	defer func() {
		if err != nil {
			result.Summary = err.Error()
		}
		_ = atomicJSON(result.ReportPath, result, 0600)
		if o.Report != "" {
			_ = atomicJSON(o.Report, result, 0600)
		}
	}()
	request, err := checkContext(w, o.Finding)
	if err != nil {
		return result, err
	}
	request.RunID = uuid()
	var best *checkAttempt
	for attempt := 0; attempt < 3; attempt++ {
		request.RequestID = uuid()
		proposal, credits, e := managedProposal(ctx, request)
		result.CreditsUsed += credits
		if e != nil {
			var badProposal proposalError
			if errors.As(e, &badProposal) {
				request.Feedback = e.Error()
				request.Previous = nil
				if best == nil {
					result.Summary = e.Error()
				}
				continue
			}
			return result, e
		}
		if e = validateProposal(proposal); e != nil {
			request.Feedback = e.Error()
			request.Previous = nil
			if best == nil {
				result.Summary = e.Error()
			}
			continue
		}
		if _, exists := w.Original[proposal.Path]; exists {
			request.Feedback = "Generated test path already exists; choose a new test file"
			request.Previous = nil
			if best == nil {
				result.Summary = request.Feedback
			}
			continue
		}
		if best != nil {
			_ = os.Remove(filepath.Join(w.Baseline, best.Path))
			_ = os.Remove(filepath.Join(w.Candidate, best.Path))
		}
		for _, root := range []string{w.Baseline, w.Candidate} {
			path, e := safePath(root, proposal.Path)
			if e != nil {
				return result, e
			}
			if e = writeBytes(path, []byte(proposal.Content), 0644); e != nil {
				return result, e
			}
		}
		w.Known[proposal.Path] = true
		command := object{"command": proposal.Command, "timeoutSeconds": 120, "purpose": "Verification test"}
		baseRun, e := runCommand(ctx, w.Baseline, command)
		if e != nil {
			return result, e
		}
		headRun, e := runCommand(ctx, w.Candidate, command)
		if e != nil {
			return result, e
		}
		current := &checkAttempt{proposal.Path, proposal.Content, proposal.Command, baseRun, headRun}
		best = current
		result.TestPath, result.TestCommand, result.Rationale = proposal.Path, proposal.Command, proposal.Rationale
		result.BaseResult, result.HeadResult = &baseRun, &headRun
		result.Verdict, result.Summary = classifyRuns(baseRun, headRun)
		request.Previous = current
		request.Feedback = ""
		if result.Verdict == "introduced_regression" || result.Verdict == "changed_behavior_verified" {
			break
		}
	}
	if best == nil {
		if result.Summary == "" {
			result.Summary = "No executable verification test was generated."
		}
		return result, nil
	}
	result.TestDiffPath = filepath.Join(w.RunDir, "test.diff")
	generatedDiff := testDiff(best.Path, best.Content)
	if e := writeBytes(result.TestDiffPath, []byte(generatedDiff), 0600); e != nil {
		return result, e
	}
	if o.AddTest || (!o.NonInteractive && !o.JSON && result.Verdict != "inconclusive") {
		apply := o.AddTest
		if !apply {
			u := newTerminalUI(os.Stdin, os.Stderr)
			u.note("Verdict", result.Summary)
			u.preview("Generated test diff", "", generatedDiff)
			answer, ok := u.choose("Add this test file to your checkout?", []string{"Yes", "No"}, false)
			apply = ok && answer == "Yes"
		}
		if apply {
			if e := applyCheckTest(w, best); e != nil {
				return result, e
			}
			result.Applied = true
		}
	}
	return result, nil
}

func checkContext(w *workspace, finding string) (checkRequest, error) {
	if len(finding) > 4000 {
		return checkRequest{}, errors.New("reviewer finding is too long; narrow the claim")
	}
	baseTree, err := snapshot(w.Baseline, w.Known)
	if err != nil {
		return checkRequest{}, err
	}
	headTree, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return checkRequest{}, err
	}
	files := []checkFile{}
	paths := changed(baseTree, headTree)
	sort.SliceStable(paths, func(i, j int) bool { return sourcePath(paths[i]) && !sourcePath(paths[j]) })
	changedSet := map[string]bool{}
	for _, p := range paths {
		changedSet[p] = true
	}
	selected := map[string]bool{}
	budget := 125000 - len(finding)
	add := func(p string) {
		if selected[p] || !relevant(p) || !safeModelPath(p) || len(files) >= 20 {
			return
		}
		candidate, e := os.ReadFile(filepath.Join(w.Candidate, p))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return
		}
		if strings.IndexByte(string(candidate), 0) >= 0 {
			return
		}
		var base []byte
		if changedSet[p] {
			base, _ = os.ReadFile(filepath.Join(w.Baseline, p))
		}
		if len(candidate)+len(base) > 30000 || len(candidate)+len(base) > budget {
			return
		}
		files = append(files, checkFile{p, string(base), string(candidate)})
		budget -= len(candidate) + len(base)
		selected[p] = true
	}
	for _, p := range paths {
		add(p)
	}
	for _, p := range []string{"go.mod", "package.json", "pyproject.toml", "pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "deno.json", "Makefile"} {
		if _, ok := headTree[p]; ok {
			add(p)
		}
	}
	stems := []string{}
	for _, p := range paths {
		if sourcePath(p) {
			stems = append(stems, strings.ToLower(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))))
		}
	}
	nearby := []string{}
	for p := range headTree {
		if selected[p] || !testPath(p) {
			continue
		}
		for _, stem := range stems {
			if len(stem) >= 3 && strings.Contains(strings.ToLower(p), stem) {
				nearby = append(nearby, p)
				break
			}
		}
	}
	sort.Strings(nearby)
	for _, p := range nearby {
		add(p)
	}
	if len(files) == 0 {
		return checkRequest{}, errors.New("no supported changed source or test files")
	}
	diffPaths := []string{}
	for _, file := range files {
		if changedSet[file.Path] {
			diffPaths = append(diffPaths, file.Path)
		}
	}
	diff := ""
	if len(diffPaths) > 0 {
		args := append([]string{"diff", "--no-ext-diff", "--no-renames", w.Base, "--"}, diffPaths...)
		patch, e := git(w.Candidate, nil, args...)
		if e != nil {
			return checkRequest{}, e
		}
		if len(patch) <= 40000 {
			diff = string(patch)
		} else {
			diff = "Unified diff omitted due to size; compare the supplied base and candidate file contents."
		}
	}
	return checkRequest{Base: w.Base, Diff: diff, Finding: finding, Files: files}, nil
}

func safeModelPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "credentials.json" || base == "id_rsa" || base == "id_ed25519" {
		return false
	}
	if strings.Contains("/"+strings.ToLower(filepath.ToSlash(path)), "/.ssh/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pem", ".key", ".p12", ".pfx", ".tfvars":
		return false
	}
	return true
}

func validateProposal(p checkProposal) error {
	allowedExt := map[string]bool{".go": true, ".js": true, ".jsx": true, ".cjs": true, ".mjs": true, ".ts": true, ".tsx": true, ".py": true, ".java": true, ".kt": true, ".rs": true, ".cs": true, ".rb": true, ".php": true, ".swift": true, ".scala": true, ".c": true, ".cpp": true}
	if !testPath(p.Path) || strings.ContainsAny(p.Path, " \t\r\n") || !allowedExt[strings.ToLower(filepath.Ext(p.Path))] || p.Content == "" || len(p.Content) > 30000 {
		return errors.New("Claude did not provide a valid new test file")
	}
	if len(p.Command) == 0 || len(p.Command) > 500 || strings.ContainsAny(p.Command, "\n\r;&|`$<>(){}\\\"'") {
		return errors.New("Claude proposed an unsafe test command")
	}
	parts := strings.Fields(p.Command)
	if len(parts) == 0 {
		return errors.New("Claude proposed no test command")
	}
	exe := filepath.Base(parts[0])
	allowed := map[string]bool{"go": true, "node": true, "npm": true, "npx": true, "pnpm": true, "yarn": true, "bun": true, "deno": true, "pytest": true, "python": true, "python3": true, "mvn": true, "mvnw": true, "gradle": true, "gradlew": true, "cargo": true, "dotnet": true, "swift": true, "ruby": true, "bundle": true, "php": true, "composer": true, "make": true}
	allowed["ctest"], allowed["bazel"], allowed["buck2"] = true, true, true
	if !allowed[exe] {
		return errors.New("Claude proposed an unsupported test runner")
	}
	for _, arg := range parts[1:] {
		if filepath.IsAbs(arg) || strings.HasPrefix(arg, "~/") || strings.Contains(arg, "=/") || arg == ".." || strings.HasPrefix(arg, "../") || strings.Contains(arg, "/../") {
			return errors.New("Claude proposed a test command outside the worktree")
		}
	}
	command := strings.ToLower(p.Command)
	if !strings.Contains(command, "test") && !strings.Contains(command, "spec") && !strings.Contains(command, "check") && !strings.Contains(command, "verify") && !strings.Contains(command, "unit") {
		return errors.New("Claude proposed a command that does not appear to run tests")
	}
	return nil
}

func classifyRuns(base, head CommandResult) (string, string) {
	if base.TimedOut || head.TimedOut {
		return "inconclusive", "Verification test timed out."
	}
	if noTestsExecuted(base.Output) || noTestsExecuted(head.Output) {
		return "inconclusive", "The test runner reported that no tests executed."
	}
	if base.ExitCode == 0 && head.ExitCode != 0 {
		return "introduced_regression", "The focused test passed on the base and failed on the change."
	}
	if base.ExitCode != 0 && head.ExitCode == 0 {
		return "changed_behavior_verified", "The focused test failed on the base and passed on the change."
	}
	if base.ExitCode == 0 && head.ExitCode == 0 {
		return "no_reproduction_found", "The attempted test passed on both revisions; this does not rule out the finding."
	}
	return "inconclusive", "The attempted test failed on both revisions; inspect the failures for a pre-existing issue or test setup problem."
}

func noTestsExecuted(output string) bool {
	lower := strings.ToLower(output)
	for _, phrase := range []string{"[no test files]", "no tests found", "no tests ran", "collected 0 items", "tests: 0\n", "0 passing"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func testDiff(path, content string) string {
	var out strings.Builder
	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	fmt.Fprintf(&out, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, path, path, lines)
	for _, line := range strings.SplitAfter(content, "\n") {
		if line == "" {
			continue
		}
		fmt.Fprint(&out, "+"+line)
		if !strings.HasSuffix(line, "\n") {
			fmt.Fprint(&out, "\n\\ No newline at end of file\n")
		}
	}
	return out.String()
}

func applyCheckTest(w *workspace, attempt *checkAttempt) error {
	now, err := snapshot(w.Root, w.Known)
	if err != nil {
		return err
	}
	if fingerprint(now) != fingerprint(w.Original) {
		return errors.New("checkout changed during verification; no test applied")
	}
	path, err := safePath(w.Root, attempt.Path)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); err == nil {
		return errors.New("test path already exists; no test applied")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeBytes(path, []byte(attempt.Content), 0644)
}
