package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type runner struct {
	mu                         sync.Mutex
	W                          *workspace
	Ask                        func(string) (string, bool)
	AskChoice                  func(string, []string, bool) (string, bool)
	UI                         *terminalUI
	BaselineOriginal, Accepted tree
	Frozen                     tree
	BaselineTest               object
	BaselineTestRevision       string
	Events                     []object
}

func (r *runner) root(phase string) string {
	if phase == "baseline" {
		return r.W.Baseline
	}
	return r.W.Candidate
}
func (r *runner) revision(phase string) (string, error) {
	t, e := snapshot(r.root(phase), r.W.Known)
	return fingerprint(t), e
}
func (r *runner) guarded(ctx context.Context, phase string, in object) (CommandResult, error) {
	root := r.root(phase)
	before, err := snapshot(root, r.W.Known)
	if err != nil {
		return CommandResult{}, err
	}
	result, err := execute(ctx, root, in)
	if err != nil {
		return result, err
	}
	after, err := snapshot(root, r.W.Known)
	if err != nil {
		return result, err
	}
	for _, p := range changed(before, after) {
		if phase == "review" || !testPath(p) || (phase == "baseline" && r.BaselineOriginal[p].Hash != "") {
			return result, fmt.Errorf("command changed protected file %s; use approved file edits during review", p)
		}
	}
	return result, nil
}
func asObject(v any) object {
	b, _ := json.Marshal(v)
	var out object
	_ = json.Unmarshal(b, &out)
	return out
}
func directEvidenceCommand(command string) error {
	if strings.ContainsAny(command, "|;&\n\r") {
		return errors.New("evidence commands must run one direct process without shell pipes or chaining; Verify captures command output itself")
	}
	return nil
}
func (r *runner) run(ctx context.Context, phase, name string, in object) (object, error) {
	root := r.root(phase)
	switch name {
	case "read_file":
		p, err := safePath(root, str(in["path"]))
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if len(b) > 256<<10 {
			return nil, errors.New("file exceeds 256 KiB; inspect a range using run_command")
		}
		return object{"content": string(b)}, nil
	case "run_command":
		result, err := r.guarded(ctx, phase, in)
		if err != nil {
			return object{"needsReview": true, "reason": err.Error()}, err
		}
		return asObject(result), nil
	case "write_file":
		rel := str(in["path"])
		p, err := safePath(root, rel)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if phase != "review" && !testPath(rel) {
			return nil, errors.New("only test files can change during characterization")
		}
		if phase == "baseline" && r.BaselineOriginal[rel].Hash != "" {
			return nil, errors.New("baseline characterization must use new test files; existing tests are immutable")
		}
		deleting := in["delete"] == true
		if deleting && phase != "review" {
			return nil, errors.New("deletions require review")
		}
		old, readErr := os.ReadFile(p)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		content := str(in["content"])
		if len(content) > 512<<10 {
			return nil, errors.New("full-file edit exceeds 512 KiB; use edit_file for exact changes in existing files")
		}
		if phase == "review" {
			r.ui().preview("Proposed edit · "+rel, str(in["explanation"]), fileDiff(old, []byte(content), deleting))
			answer, ok := r.approveEdit()
			if !ok {
				return object{"needsReview": true, "reason": "User approval is required for proposed edits"}, nil
			}
			if !yes(answer) {
				return object{"accepted": false, "feedback": answer}, errors.New("edit was not accepted")
			}
		}
		r.W.Known[rel] = true
		if deleting {
			err = os.Remove(p)
		} else {
			mode := os.FileMode(0644)
			if info, e := os.Stat(p); e == nil {
				mode = info.Mode().Perm()
			}
			err = writeBytes(p, []byte(content), mode)
		}
		return object{"accepted": err == nil, "path": rel}, err
	case "edit_file":
		rel := filepath.ToSlash(filepath.Clean(str(in["path"])))
		p, err := safePath(root, rel)
		if err != nil {
			return nil, err
		}
		if phase != "review" && !testPath(rel) {
			return nil, errors.New("only test files can change during characterization")
		}
		if phase == "baseline" && r.BaselineOriginal[rel].Hash != "" {
			return nil, errors.New("baseline characterization must use new test files; existing tests are immutable")
		}
		old, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		before, after := []byte(str(in["oldText"])), []byte(str(in["newText"]))
		if len(before) == 0 || bytes.Equal(before, after) || bytes.Count(old, before) != 1 {
			return nil, errors.New("edit_file requires one unique, nonempty oldText and a changed replacement")
		}
		updated := bytes.Replace(old, before, after, 1)
		if phase == "review" {
			r.ui().preview("Proposed edit · "+rel, str(in["explanation"]), fileDiff(old, updated, false))
			answer, ok := r.approveEdit()
			if !ok {
				return object{"needsReview": true, "reason": "User approval is required for proposed edits"}, nil
			}
			if !yes(answer) {
				return object{"accepted": false, "feedback": answer}, errors.New("edit was not accepted")
			}
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		err = writeBytes(p, updated, info.Mode().Perm())
		return object{"accepted": err == nil, "path": rel}, err
	case "measure":
		kind := str(in["kind"])
		if kind != "tests" && kind != "coverage" && kind != "mutation" {
			return nil, errors.New("invalid measurement kind")
		}
		if err := directEvidenceCommand(str(in["command"])); err != nil {
			return nil, err
		}
		in["purpose"] = "Measure " + phase + " " + kind
		scope := r.W.FinalScope
		if phase == "baseline" {
			scope = r.W.BaselineScope
		}
		reportPath := ""
		if kind != "tests" {
			if len(scope) == 0 {
				return object{"kind": kind, "files": []string{}, "counters": []counter{}, "notApplicable": true}, nil
			}
			var err error
			reportPath, err = safePath(root, str(in["report"]))
			if err != nil {
				return nil, err
			}
			current, err := snapshot(root, r.W.Known)
			if err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(root, reportPath)
			if _, ok := current[filepath.ToSlash(rel)]; ok {
				return nil, errors.New("measurement output must be a generated report, not a tracked/source file")
			}
			if err = os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		result, err := r.guarded(ctx, phase, in)
		if err != nil {
			return object{"needsReview": true, "reason": err.Error()}, err
		}
		out := asObject(result)
		out["kind"] = kind
		if result.ExitCode != 0 || result.TimedOut {
			return out, nil
		}
		if kind == "tests" {
			if phase == "baseline" {
				r.BaselineTest = in
				r.BaselineTestRevision, _ = r.revision(phase)
			}
			return out, nil
		}
		data, err := os.ReadFile(reportPath)
		if err != nil {
			return out, fmt.Errorf("fresh native report missing: %w", err)
		}
		if len(data) > 32<<20 {
			return out, errors.New("native report exceeds 32 MiB")
		}
		format := str(in["format"])
		if kind == "mutation" && format != "pit" && format != "stryker" {
			return out, errors.New("mutation measurement requires pit or stryker")
		}
		if kind == "coverage" && (format == "pit" || format == "stryker") {
			return out, errors.New("coverage measurement requires a coverage format")
		}
		m, err := parseMetrics(data, format, root, scope)
		if err != nil {
			return out, err
		}
		for k, v := range asObject(m) {
			out[k] = v
		}
		out["report"] = str(in["report"])
		out["format"] = format
		copyPath := filepath.Join(r.W.RunDir, "evidence", uuid()+"-"+filepath.Base(reportPath))
		if err = writeBytes(copyPath, data, 0600); err != nil {
			return out, err
		}
		out["artifact"] = copyPath
		return out, nil
	case "replay_baseline":
		if phase != "baseline" {
			return nil, errors.New("replay is stage 1 only")
		}
		revision, err := r.revision("baseline")
		if err != nil {
			return nil, err
		}
		if r.BaselineTest == nil || r.BaselineTestRevision != revision {
			return nil, errors.New("run passing baseline tests on this revision first")
		}
		if err := directEvidenceCommand(str(in["existingTestsCommand"])); err != nil {
			return nil, err
		}
		for p, v := range r.Frozen {
			current, e := snapshot(r.W.Candidate, r.W.Known)
			if e != nil {
				return nil, e
			}
			if current[p] != v {
				return nil, fmt.Errorf("frozen candidate test changed: %s", p)
			}
			if e = os.Remove(filepath.Join(r.W.Candidate, p)); e != nil {
				return nil, e
			}
		}
		existingSuite := object{
			"command":        str(in["existingTestsCommand"]),
			"timeoutSeconds": in["timeoutSeconds"],
			"purpose":        "Run existing candidate test suite",
		}
		originalTests, err := r.guarded(ctx, "review", existingSuite)
		if err != nil {
			return object{"needsReview": true}, err
		}
		baseline, err := snapshot(r.W.Baseline, r.W.Known)
		if err != nil {
			return nil, err
		}
		r.Frozen = tree{}
		for _, p := range changed(r.BaselineOriginal, baseline) {
			if !testPath(p) || r.BaselineOriginal[p].Hash != "" {
				return nil, fmt.Errorf("baseline must only add characterization tests: %s", p)
			}
			dest, e := safePath(r.W.Candidate, p)
			if e != nil {
				return nil, e
			}
			if _, e = os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
				return nil, fmt.Errorf("characterization filename conflicts with candidate: %s; choose a new filename", p)
			}
			data, e := os.ReadFile(filepath.Join(r.W.Baseline, p))
			if e != nil {
				return nil, e
			}
			if e = writeBytes(dest, data, baseline[p].Mode); e != nil {
				return nil, e
			}
			r.W.Known[p] = true
			r.Frozen[p] = baseline[p]
		}
		if len(r.W.BaselineScope) > 0 && len(r.Frozen) == 0 {
			return nil, errors.New("add characterization tests before replay")
		}
		characterization := object{
			"command":        r.BaselineTest["command"],
			"timeoutSeconds": r.BaselineTest["timeoutSeconds"],
			"purpose":        "Replay baseline characterization tests",
		}
		result, err := r.guarded(ctx, "review", characterization)
		if err != nil {
			return object{"needsReview": true}, err
		}
		exitCode := result.ExitCode
		if originalTests.ExitCode != 0 && exitCode == 0 {
			exitCode = originalTests.ExitCode
		}
		return object{"kind": "replay", "baselineRevision": revision, "existingTests": originalTests, "characterizationTests": result, "frozenFiles": r.Frozen, "exitCode": exitCode}, nil
	case "ask_user":
		choices := []string{}
		for _, option := range list(in["options"]) {
			if value := strings.TrimSpace(str(option)); value != "" {
				choices = append(choices, value)
			}
		}
		if len(choices) < 2 || len(choices) > 4 {
			return object{"needsReview": true, "reason": "The agent must provide two to four answer options"}, nil
		}
		answer, ok := r.askChoice(str(in["question"]), choices, true)
		if !ok {
			return object{"needsReview": true, "reason": "Intent clarification needs user input"}, nil
		}
		return object{"answer": answer}, nil
	case "finish_stage":
		if phase == "review" {
			diff, err := r.W.diff()
			if err != nil {
				return nil, err
			}
			preview, err := r.W.reviewDiff("review", diff)
			if err != nil {
				return nil, err
			}
			r.ui().preview("Review the complete candidate diff", str(in["summary"]), preview)
			before, err := snapshot(root, r.W.Known)
			if err != nil {
				return nil, err
			}
			answer, ok := r.approveReview()
			if !ok {
				return object{"needsReview": true, "reason": "Stage 2 requires explicit user acceptance"}, nil
			}
			after, err := snapshot(root, r.W.Known)
			if err != nil {
				return nil, err
			}
			if fingerprint(before) != fingerprint(after) {
				return nil, errors.New("candidate changed during review; review it again")
			}
			accepted := yes(answer)
			if accepted {
				r.Accepted = after
				r.W.FinalScope = nil
				for _, p := range changed(r.BaselineOriginal, after) {
					if sourcePath(p) {
						if _, ok := after[p]; ok {
							r.W.FinalScope = append(r.W.FinalScope, p)
						}
					}
				}
			}
			return object{"accepted": accepted, "feedback": answer, "scope": r.W.FinalScope}, nil
		}
		return object{"completed": true}, nil
	case "block":
		return object{"reason": str(in["reason"])}, nil
	case "finish_report":
		return object{"completed": true}, nil
	default:
		return nil, fmt.Errorf("unknown backend tool %q", name)
	}
}
func (r *runner) tool(ctx context.Context, phase string, call object) object {
	name := str(call["name"])
	result, err := r.run(ctx, phase, name, obj(call["input"]))
	if result == nil {
		result = object{}
	}
	if phase != "report" {
		revision, e := r.revision(phase)
		if e == nil {
			result["revision"] = revision
		} else if err == nil {
			err = e
		}
		if phase == "baseline" {
			current, e := snapshot(r.W.Baseline, r.W.Known)
			if e != nil {
				err = e
			} else {
				for _, p := range changed(r.BaselineOriginal, current) {
					if !testPath(p) || r.BaselineOriginal[p].Hash != "" {
						err = fmt.Errorf("baseline changed protected file %s", p)
						result["needsReview"] = true
						break
					}
				}
			}
		}
		if phase == "lock" && r.Accepted != nil {
			current, e := snapshot(r.W.Candidate, r.W.Known)
			if e == nil {
				for _, p := range changed(r.Accepted, current) {
					if !testPath(p) {
						err = fmt.Errorf("accepted production/configuration changed during stage 3: %s", p)
						result["needsReview"] = true
						break
					}
				}
			} else {
				err = e
			}
		}
	}
	out := object{"toolUseId": str(call["id"]), "value": result}
	if err != nil {
		out["isError"] = true
		result["error"] = err.Error()
		r.ui().problem(err.Error())
	}
	r.mu.Lock()
	r.Events = append(r.Events, object{"phase": phase, "tool": name, "input": obj(call["input"]), "result": out})
	r.mu.Unlock()
	return out
}
func yes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes" || s == "accept"
}
func (r *runner) ui() *terminalUI {
	if r.UI != nil {
		return r.UI
	}
	return newTerminalUI(os.Stdin, os.Stderr)
}
func (r *runner) askChoice(question string, options []string, comment bool) (string, bool) {
	if r.AskChoice != nil {
		return r.AskChoice(question, options, comment)
	}
	if r.Ask != nil {
		return r.Ask(question)
	}
	return "", false
}
func (r *runner) approveEdit() (string, bool) {
	answer, ok := r.askChoice("What should happen to this edit?", []string{"Apply this edit", "Revise this edit", "Skip this edit"}, true)
	switch answer {
	case "Apply this edit":
		return "yes", ok
	case "Revise this edit":
		return "Please propose a different edit. Ask me a focused question with options if you need direction.", ok
	case "Skip this edit":
		return "Do not apply this edit. Continue with the remaining changes.", ok
	default:
		return answer, ok
	}
}
func (r *runner) approveReview() (string, bool) {
	answer, ok := r.askChoice("Accept this complete candidate and run final tests?", []string{"Yes", "No"}, true)
	switch answer {
	case "Yes":
		return "yes", ok
	case "No":
		return "Please revise the candidate. Ask me a focused question with options if you need direction.", ok
	default:
		return answer, ok
	}
}
func fileDiff(old, new []byte, deleting bool) string {
	// git's no-index diff handles normal contextual diffs without changing either worktree.
	dir, err := os.MkdirTemp("", "verify-preview-")
	if err != nil {
		return string(new)
	}
	defer os.RemoveAll(dir)
	_ = os.WriteFile(filepath.Join(dir, "before"), old, 0600)
	_ = os.WriteFile(filepath.Join(dir, "after"), new, 0600)
	command := exec.Command("git", "-c", "core.quotePath=false", "diff", "--no-index", "--no-ext-diff", "--", "before", "after")
	command.Dir = dir
	b, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "Could not render diff; proposed file follows:\n" + string(new)
		}
	}
	if deleting {
		return "Delete file\n" + string(b)
	}
	return string(b)
}
