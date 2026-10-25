package verify

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

type options struct {
	Repo, Base, Report, Agent string
	NonInteractive, JSON      bool
}

type stageResult struct {
	Summary              string   `json:"summary"`
	TestCommand          string   `json:"testCommand"`
	ExistingSuiteCommand string   `json:"existingSuiteCommand"`
	CoverageCommand      string   `json:"coverageCommand"`
	CoverageReport       string   `json:"coverageReport"`
	CoverageFormat       string   `json:"coverageFormat"`
	Question             string   `json:"question"`
	Options              []string `json:"options"`
	Blocker              string   `json:"blocker"`
}

func Main(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	if len(args) == 1 && args[0] == "update" {
		result, err := checkAndInstallUpdate(ctx, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Verify update failed:", err)
			return 2
		}
		if !result.Updated {
			fmt.Println("Verify", Version, "is up to date")
		} else if result.Scheduled {
			fmt.Println("Verify", result.Version, "will be installed after this process exits; run verify again")
		} else {
			fmt.Println("Updated Verify to", result.Version+"; the next run uses it")
		}
		return 0
	}
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	o := options{}
	flags.StringVar(&o.Repo, "repo", ".", "Git repository to verify")
	flags.StringVar(&o.Base, "base", "", "Baseline ref (default: merge-base with origin/main or main)")
	flags.StringVar(&o.Report, "report", "", "Also save the JSON report at this path")
	flags.StringVar(&o.Agent, "agent", "auto", "Installed coding agent: auto, codex, or claude")
	flags.BoolVar(&o.NonInteractive, "non-interactive", false, "Stop when an intent or approval decision needs input")
	flags.BoolVar(&o.JSON, "json", false, "Print the final structured report as JSON")
	version := flags.Bool("version", false, "Show version")
	flags.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: verify [--agent auto|codex|claude] [--repo PATH] [--base REF] [--json]\n       verify update\n\nUses your installed coding agent and local project tools. Checks for updates on each run.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Println(Version)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Unexpected arguments")
		return 2
	}
	update, updateErr := checkAndInstallUpdate(ctx, false)
	if updateErr != nil {
		fmt.Fprintln(os.Stderr, "Verify update warning:", updateErr)
	} else if update.Updated {
		if update.Scheduled {
			fmt.Fprintln(os.Stderr, "Verify update scheduled for after this run:", update.Version)
		} else {
			fmt.Fprintln(os.Stderr, "Verify updated to", update.Version+"; this run continues on", Version)
		}
	}
	agent, err := selectAgent(o.Agent)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	o.Agent = agent
	report, err := verify(ctx, o)
	if err != nil {
		newTerminalUI(os.Stdin, os.Stderr).problem(err.Error())
	}
	if report != nil {
		if o.JSON {
			fmt.Println(jsonText(report))
		} else {
			newTerminalUI(os.Stdin, os.Stdout).report(str(report["report"]))
			newTerminalUI(os.Stdin, os.Stderr).note("Report", str(report["reportPath"]))
		}
	}
	if err != nil {
		return 2
	}
	if report["verdict"] != "good_to_go" {
		return 1
	}
	return 0
}

func verify(ctx context.Context, o options) (report object, err error) {
	started := time.Now()
	w, err := prepare(o.Repo, o.Base)
	if err != nil {
		return nil, err
	}
	u := newTerminalUI(os.Stdin, os.Stderr)
	report = object{
		"version": 2, "agent": o.Agent, "verdict": "not_ready", "base": w.Base,
		"repository": w.Root, "baselineScope": w.BaselineScope, "finalScope": w.FinalScope,
		"workspaces": object{"baseline": w.Baseline, "candidate": w.Candidate},
		"reportPath": filepath.Join(w.RunDir, "report.json"), "applied": false,
	}
	defer func() {
		report["durationMs"] = time.Since(started).Milliseconds()
		report["finalScope"] = w.FinalScope
		if err != nil {
			report["verdict"] = "not_ready"
			report["error"] = err.Error()
			report["report"] = "# Verify: not ready\n\nVerification stopped: " + err.Error() + "\n\nThe worktrees and logs are retained at `" + w.RunDir + "`."
		}
		if e := atomicJSON(str(report["reportPath"]), report, 0600); e != nil && err == nil {
			err = e
		}
		_ = writeBytes(filepath.Join(w.RunDir, "report.md"), []byte(str(report["report"])), 0600)
		if o.Report != "" {
			if e := atomicJSON(o.Report, report, 0600); e != nil && err == nil {
				err = e
			}
		}
	}()

	baselineOriginal, err := snapshot(w.Baseline, w.Known)
	if err != nil {
		return report, err
	}
	u.stage("1", "Characterize the baseline")
	baseline, err := runAgentStage(ctx, o.Agent, w.Baseline, w.RunDir, "baseline", promptBaseline(w))
	if err != nil {
		return report, err
	}
	report["baseline"] = baseline
	if baseline.Blocker != "" {
		return blocked(report, "Baseline: "+baseline.Blocker), nil
	}
	baselineNow, err := snapshot(w.Baseline, w.Known)
	if err != nil {
		return report, err
	}
	newTests := []string{}
	for _, p := range changed(baselineOriginal, baselineNow) {
		if !testPath(p) || baselineOriginal[p].Hash != "" {
			return report, fmt.Errorf("baseline agent changed protected file %s", p)
		}
		newTests = append(newTests, p)
	}
	if len(w.BaselineScope) > 0 && len(newTests) == 0 {
		return blocked(report, "No characterization tests were added for existing changed code."), nil
	}
	if baseline.TestCommand == "" || baseline.ExistingSuiteCommand == "" {
		return blocked(report, "The baseline agent did not supply test and full-suite commands."), nil
	}
	baselineTest, e := runEvidence(ctx, w.Baseline, baseline.TestCommand, "Baseline characterization tests")
	if e != nil {
		return report, e
	}
	report["baselineTests"] = baselineTest
	if baselineTest.ExitCode != 0 {
		return blocked(report, "Characterization tests failed on the baseline."), nil
	}
	if err = assertUnchanged(w.Baseline, w.Known, baselineNow); err != nil {
		return report, fmt.Errorf("baseline tests changed source or test files: %w", err)
	}
	baselineSuite, e := runEvidence(ctx, w.Baseline, baseline.ExistingSuiteCommand, "Baseline full suite")
	if e != nil {
		return report, e
	}
	report["baselineSuite"] = baselineSuite
	if baselineSuite.ExitCode != 0 {
		return blocked(report, "The existing suite failed on the baseline."), nil
	}
	if err = assertUnchanged(w.Baseline, w.Known, baselineNow); err != nil {
		return report, fmt.Errorf("baseline suite changed source or test files: %w", err)
	}
	candidateBeforeReplay, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return report, err
	}
	candidateSuite, e := runEvidence(ctx, w.Candidate, baseline.ExistingSuiteCommand, "Candidate existing suite")
	if e != nil {
		return report, e
	}
	report["candidateExistingSuite"] = candidateSuite
	if err = assertUnchanged(w.Candidate, w.Known, candidateBeforeReplay); err != nil {
		return report, fmt.Errorf("existing suite changed candidate files: %w", err)
	}
	for _, p := range newTests {
		if _, e = os.Lstat(filepath.Join(w.Candidate, p)); !errors.Is(e, os.ErrNotExist) {
			return report, fmt.Errorf("characterization test conflicts with candidate file %s", p)
		}
		data, e := os.ReadFile(filepath.Join(w.Baseline, p))
		if e != nil {
			return report, e
		}
		if e = writeBytes(filepath.Join(w.Candidate, p), data, baselineNow[p].Mode); e != nil {
			return report, e
		}
		w.Known[p] = true
	}
	candidateWithTests, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return report, err
	}
	replay, e := runEvidence(ctx, w.Candidate, baseline.TestCommand, "Replay old behavior on candidate")
	if e != nil {
		return report, e
	}
	report["replay"] = replay
	report["frozenTests"] = newTests
	if err = assertUnchanged(w.Candidate, w.Known, candidateWithTests); err != nil {
		return report, fmt.Errorf("replay changed candidate files: %w", err)
	}

	u.stage("2", "Review intent and edits")
	answers := []object{}
	var review stageResult
	for attempt := 0; attempt < 4; attempt++ {
		before, e := snapshot(w.Candidate, w.Known)
		if e != nil {
			return report, e
		}
		review, e = runAgentStage(ctx, o.Agent, w.Candidate, w.RunDir, fmt.Sprintf("review-%d", attempt+1), promptReview(w, report, answers))
		if e != nil {
			return report, e
		}
		if review.Blocker != "" {
			return blocked(report, "Review: "+review.Blocker), nil
		}
		if review.Question == "" {
			break
		}
		if e = assertUnchanged(w.Candidate, w.Known, before); e != nil {
			return report, fmt.Errorf("agent edited files before an intent answer: %w", e)
		}
		if len(review.Options) < 2 || len(review.Options) > 4 {
			return report, errors.New("agent question needs two to four answer options")
		}
		if o.NonInteractive {
			report["question"] = review.Question
			return needsReview(report, "Intent decision needed: "+review.Question), nil
		}
		answer, ok := u.choose(review.Question, review.Options, true)
		if !ok {
			report["question"] = review.Question
			return needsReview(report, "Intent decision needed: "+review.Question), nil
		}
		answers = append(answers, object{"question": review.Question, "answer": answer})
		if attempt == 3 {
			return blocked(report, "The agent did not resolve intent after four questions."), nil
		}
	}
	report["decisions"] = answers
	report["review"] = review
	diff, err := w.diff()
	if err != nil {
		return report, err
	}
	preview, err := w.reviewDiff("review", diff)
	if err != nil {
		return report, err
	}
	u.preview("Review the complete candidate diff", review.Summary, preview)
	accepted, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return report, err
	}
	if o.NonInteractive {
		return needsReview(report, "The candidate diff needs your approval."), nil
	}
	answer, ok := u.choose("Accept this candidate behavior and proceed to final tests?", []string{"Yes", "No"}, false)
	if !ok || answer != "Yes" {
		return needsReview(report, "The candidate diff was not accepted."), nil
	}
	if err = assertUnchanged(w.Candidate, w.Known, accepted); err != nil {
		return report, fmt.Errorf("candidate changed during review: %w", err)
	}
	w.FinalScope = sourceChanges(baselineOriginal, accepted)

	u.stage("3", "Test accepted behavior")
	lock, err := runAgentStage(ctx, o.Agent, w.Candidate, w.RunDir, "lock", promptLock(w, report))
	if err != nil {
		return report, err
	}
	report["lock"] = lock
	if lock.Blocker != "" {
		return blocked(report, "Final tests: "+lock.Blocker), nil
	}
	finalTree, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return report, err
	}
	for _, p := range changed(accepted, finalTree) {
		if !testPath(p) {
			return report, fmt.Errorf("agent changed accepted non-test file during final tests: %s", p)
		}
		if _, exists := finalTree[p]; !exists {
			return report, fmt.Errorf("agent deleted accepted test file during final tests: %s", p)
		}
	}
	if lock.TestCommand == "" {
		return blocked(report, "The agent did not supply a full-suite test command."), nil
	}
	if lock.TestCommand != baseline.ExistingSuiteCommand {
		focused, e := runEvidence(ctx, w.Candidate, lock.TestCommand, "Final agent-selected tests")
		if e != nil {
			return report, e
		}
		report["finalAgentTests"] = focused
		if focused.ExitCode != 0 {
			return blocked(report, "The agent-selected final tests failed."), nil
		}
		if err = assertUnchanged(w.Candidate, w.Known, finalTree); err != nil {
			return report, fmt.Errorf("agent-selected final tests changed source or test files: %w", err)
		}
	}
	finalTests, e := runEvidence(ctx, w.Candidate, baseline.ExistingSuiteCommand, "Final full test suite")
	if e != nil {
		return report, e
	}
	report["finalTests"] = finalTests
	if finalTests.ExitCode != 0 {
		return blocked(report, "The final test suite failed."), nil
	}
	if err = assertUnchanged(w.Candidate, w.Known, finalTree); err != nil {
		return report, fmt.Errorf("final tests changed source or test files: %w", err)
	}
	if lock.CoverageCommand != "" {
		coverage, e := runCoverage(ctx, w, lock)
		if e != nil {
			return blocked(report, "Coverage could not be measured: "+e.Error()), nil
		}
		report["coverage"] = coverage
	} else {
		report["coverage"] = "not measured"
	}
	if err = assertUnchanged(w.Candidate, w.Known, finalTree); err != nil {
		return report, fmt.Errorf("coverage command changed source or test files: %w", err)
	}
	u.stage("4", "Prepare the report")
	report["verdict"] = "good_to_go"
	report["report"] = formatLocalReport(report)
	verified, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return report, err
	}
	diff, err = w.diff()
	if err != nil {
		return report, err
	}
	preview, err = w.reviewDiff("final", diff)
	if err != nil {
		return report, err
	}
	u.preview("Final tested diff", "", preview)
	if !o.NonInteractive {
		answer, ok = u.choose("Apply this tested diff to your checkout?", []string{"Yes", "No"}, false)
		if ok && answer == "Yes" {
			if err = assertUnchanged(w.Candidate, w.Known, verified); err != nil {
				return report, fmt.Errorf("candidate changed during final review: %w", err)
			}
			if err = w.promote(); err != nil {
				return report, err
			}
			report["applied"] = true
		}
	}
	return report, nil
}

func assertUnchanged(root string, known map[string]bool, before tree) error {
	after, err := snapshot(root, known)
	if err != nil {
		return err
	}
	if fingerprint(before) != fingerprint(after) {
		return fmt.Errorf("changed files: %s", strings.Join(changed(before, after), ", "))
	}
	return nil
}

func sourceChanges(old, current tree) []string {
	out := []string{}
	for _, p := range changed(old, current) {
		if sourcePath(p) && current[p].Hash != "" {
			out = append(out, p)
		}
	}
	return out
}

func blocked(report object, reason string) object {
	report["verdict"] = "not_ready"
	report["report"] = "# Verify: not ready\n\n" + reason
	return report
}

func needsReview(report object, reason string) object {
	report["verdict"] = "needs_review"
	report["report"] = "# Verify: needs review\n\n" + reason
	return report
}

func formatLocalReport(report object) string {
	baseline := report["baseline"].(stageResult)
	review := report["review"].(stageResult)
	lock := report["lock"].(stageResult)
	replay := report["replay"].(CommandResult)
	candidate := report["candidateExistingSuite"].(CommandResult)
	final := report["finalTests"].(CommandResult)
	status := func(result CommandResult) string {
		if result.ExitCode == 0 {
			return "passed"
		}
		return fmt.Sprintf("failed (exit %d)", result.ExitCode)
	}
	decisionText := ""
	if decisions, ok := report["decisions"].([]object); ok {
		for _, decision := range decisions {
			decisionText += fmt.Sprintf("- %s %s\n", str(decision["question"]), str(decision["answer"]))
		}
	}
	if decisionText == "" {
		decisionText = "No intent question was needed.\n"
	}
	extraTests := ""
	if value, ok := report["finalAgentTests"].(CommandResult); ok {
		extraTests = fmt.Sprintf("- Additional final tests: %s (`%s`)\n", status(value), value.Command)
	}
	return fmt.Sprintf("# Verify: good to go\n\n**Verdict:** `good_to_go`\n\n%s\n\n## Evidence\n\n- Baseline characterization: %s (`%s`)\n- Baseline full suite: %s (`%s`)\n- Existing candidate suite: %s (`%s`)\n- Old behavior replay: %s (`%s`)\n%s- Final full suite: %s (`%s`)\n- Coverage: %v\n\n## Intent decisions\n\n%s\n## Review\n\n%s\n\n## New behavior\n\n%s\n\nThe tested diff is available in the retained candidate worktree. A passing suite only covers behavior those tests exercise.",
		baseline.Summary, status(report["baselineTests"].(CommandResult)), report["baselineTests"].(CommandResult).Command,
		status(report["baselineSuite"].(CommandResult)), report["baselineSuite"].(CommandResult).Command,
		status(candidate), candidate.Command, status(replay), replay.Command, extraTests, status(final), final.Command,
		coverageText(report["coverage"]), decisionText, review.Summary, lock.Summary)
}

func coverageText(value any) string {
	measured, ok := value.(map[string]any)
	if !ok {
		return str(value)
	}
	m, ok := measured["metrics"].(metrics)
	if !ok {
		return "measurement unavailable"
	}
	parts := make([]string, 0, len(m.Counters))
	for _, c := range m.Counters {
		parts = append(parts, fmt.Sprintf("%s %s %d/%d", c.File, c.Metric, c.Covered, c.Total))
	}
	if len(parts) == 0 {
		return "no affected source counters"
	}
	return strings.Join(parts, "; ")
}

func parseStageResult(path string) (stageResult, error) {
	var result stageResult
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("agent did not write its stage result: %w", err)
	}
	defer os.Remove(path)
	if len(data) > 1<<20 {
		return result, errors.New("agent stage result exceeds 1 MiB")
	}
	if err = decodeJSON(strings.NewReader(string(data)), &result); err != nil {
		return result, fmt.Errorf("invalid agent stage result: %w", err)
	}
	if result.Summary == "" && result.Blocker == "" && result.Question == "" {
		return result, errors.New("agent stage result needs a summary, question, or blocker")
	}
	return result, nil
}
