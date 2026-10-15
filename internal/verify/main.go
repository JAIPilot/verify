package verify

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const endpoint = "https://otxfylhjrlaesjagfhfi.supabase.co/functions/v1/invoke-verify-agent"

type options struct {
	Repo, Base, Report   string
	NonInteractive, JSON bool
	MaxTurns             int
}

func Main(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	if len(args) > 0 && args[0] == "auth" {
		if err := auth(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	o := options{}
	flags.StringVar(&o.Repo, "repo", ".", "Git repository to verify")
	flags.StringVar(&o.Base, "base", "", "Baseline ref (default: merge-base with origin/main or main)")
	flags.StringVar(&o.Report, "report", "", "Also save the JSON report at this path")
	flags.BoolVar(&o.NonInteractive, "non-interactive", false, "Stop with needs_review when user input is required")
	flags.BoolVar(&o.JSON, "json", false, "Print the final structured report as JSON")
	flags.IntVar(&o.MaxTurns, "max-turns", 200, "Maximum model turns before reporting incomplete verification")
	version := flags.Bool("version", false, "Show version")
	flags.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: verify [--repo PATH] [--base REF] [--json]\n       verify auth login|status|logout\n\nFour stages: characterize baseline → review and accept → lock behavior → report.\nBuilds and tests run locally. JAIPilot runs three Claude agents in its backend.\n\n")
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
	if flags.NArg() != 0 || o.MaxTurns < 1 || o.MaxTurns > 1000 {
		fmt.Fprintln(os.Stderr, "Unexpected arguments or invalid --max-turns (1–1000)")
		return 2
	}
	if _, err := bearerToken(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	report, err := verify(ctx, o)
	if err != nil {
		newTerminalUI(os.Stdin, os.Stderr).problem(err.Error())
	}
	if report != nil {
		if o.JSON {
			fmt.Println(jsonText(report))
		} else {
			output := newTerminalUI(os.Stdin, os.Stdout)
			if output.terminal {
				output.report(str(report["report"]))
			} else {
				fmt.Println(str(report["report"]))
			}
			status := newTerminalUI(os.Stdin, os.Stderr)
			status.note("Report", str(report["reportPath"]))
			if u, ok := report["usage"].(map[string]any); ok {
				cost, _ := u["estimatedProviderUsd"].(float64)
				elapsed, _ := report["durationMs"].(int64)
				status.note("Usage", fmt.Sprintf("%s  ·  %d model turns  ·  $%.4f estimated provider cost", (time.Duration(elapsed)*time.Millisecond).Round(time.Second), num(u["modelTurns"]), cost))
			}
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
func auth(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: verify auth login|status|logout")
	}
	switch args[0] {
	case "login":
		email, err := login(ctx)
		if err == nil {
			fmt.Println("Signed in to JAIPilot as", email)
		}
		return err
	case "status":
		session, err := readSession()
		if err != nil {
			return err
		}
		if session == nil {
			fmt.Println("Not signed in")
			return nil
		}
		if _, err = bearerToken(ctx); err != nil {
			return err
		}
		fmt.Println("Signed in to JAIPilot as", session.Email)
		return nil
	case "logout":
		if err := logout(); err != nil {
			return err
		}
		fmt.Println("Signed out of JAIPilot (shared with jaipilot)")
		return nil
	}
	return errors.New("usage: verify auth login|status|logout")
}
func service(ctx context.Context, body object) (object, error) {
	token, err := bearerToken(ctx)
	if err != nil {
		return nil, err
	}
	address := os.Getenv("JAIPILOT_VERIFY_ENDPOINT")
	if address == "" {
		address = endpoint
	}
	data := jsonText(body)
	deadline := time.Now().Add(10 * time.Minute)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, 140*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, "POST", address, strings.NewReader(data))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, requestErr := http.DefaultClient.Do(req)
		value := object{}
		status := 0
		if requestErr == nil {
			status = response.StatusCode
			err = json.NewDecoder(io.LimitReader(response.Body, 2_000_001)).Decode(&value)
			response.Body.Close()
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if requestErr == nil && err == nil && status == 200 {
			return value, nil
		}
		retry := requestErr != nil || status == 202 || status == 408 || status == 425 || status == 429 || status >= 500
		if !retry || value["retryable"] == false {
			if str(value["error"]) == "" {
				return nil, fmt.Errorf("verification service returned HTTP %d", status)
			}
			if detail := str(value["providerMessage"]); detail != "" {
				return nil, fmt.Errorf("%s: %s", str(value["error"]), detail)
			}
			return nil, errors.New(str(value["error"]))
		}
		delay := time.Duration(2+attempt) * time.Second
		if delay > 15*time.Second {
			delay = 15 * time.Second
		}
		newTerminalUI(os.Stdin, os.Stderr).activity("Waiting for the JAIPilot service…")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("verification request timed out; worktrees and evidence are retained")
}

var requestService = service

func verify(ctx context.Context, o options) (report object, err error) {
	started := time.Now()
	w, err := prepare(o.Repo, o.Base)
	if err != nil {
		return nil, err
	}
	r := &runner{W: w}
	r.BaselineOriginal, err = snapshot(w.Baseline, w.Known)
	if err != nil {
		return nil, err
	}
	u := newTerminalUI(os.Stdin, os.Stderr)
	r.UI = u
	r.AskChoice = func(question string, choices []string, comment bool) (string, bool) {
		if o.NonInteractive {
			return "", false
		}
		return u.choose(question, choices, comment)
	}
	usage := object{"modelTurns": 0, "inputTokens": 0, "outputTokens": 0, "cacheReadTokens": 0, "cacheWriteTokens": 0, "estimatedProviderUsd": 0.0}
	report = object{"version": 1, "verdict": "not_ready", "base": w.Base, "repository": w.Root, "baselineScope": w.BaselineScope, "finalScope": w.FinalScope, "workspaces": object{"baseline": w.Baseline, "candidate": w.Candidate}, "reportPath": filepath.Join(w.RunDir, "report.json"), "applied": false, "usage": usage}
	defer func() {
		report["durationMs"] = time.Since(started).Milliseconds()
		report["events"] = r.Events
		report["finalScope"] = w.FinalScope
		if err != nil {
			report["verdict"] = "not_ready"
			report["error"] = err.Error()
			report["report"] = "# Verify: not ready\n\nVerification did not complete: " + err.Error() + "\n\nIsolated worktrees and evidence are retained for inspection."
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
	body := object{"requestId": uuid(), "context": object{"base": w.Base, "baselineScope": nonNil(w.BaselineScope), "finalScope": nonNil(w.FinalScope), "platform": platform(), "instructions": "Read project instructions from the worktree. The diff stays local; use shell and file tools to inspect it. All source paths and commands are relative to the active workspace."}}
	lastPhase := ""
	for turn := 0; turn < o.MaxTurns+8; turn++ {
		if e := atomicJSON(filepath.Join(w.RunDir, "request.json"), body, 0600); e != nil {
			return report, e
		}
		response, e := requestService(ctx, body)
		if e != nil {
			return report, e
		}
		if u, ok := response["usage"].(map[string]any); ok {
			usage["modelTurns"] = num(usage["modelTurns"]) + 1
			for _, field := range []string{"inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens"} {
				usage[field] = num(usage[field]) + num(u[field])
			}
			if cost, e := strconv.ParseFloat(str(u["providerUsd"]), 64); e == nil && cost >= 0 {
				usage["estimatedProviderUsd"] = usage["estimatedProviderUsd"].(float64) + cost
			}
		}
		phase := str(response["phase"])
		if phase != lastPhase {
			labels := map[string][2]string{"baseline": {"1", "Characterize the baseline"}, "review": {"2", "Review intent and edits"}, "lock": {"3", "Lock accepted behavior"}, "report": {"4", "Prepare the report"}}
			if label, ok := labels[phase]; ok {
				u.stage(label[0], label[1])
			}
			lastPhase = phase
		}
		if phase == "done" {
			report["verdict"] = response["verdict"]
			report["report"] = response["report"]
			report["evidence"] = response["evidence"]
			report["decisions"] = response["decisions"]
			if report["verdict"] == "good_to_go" {
				verified, e := snapshot(w.Candidate, w.Known)
				if e != nil {
					return report, e
				}
				if fingerprint(verified) != str(response["verifiedRevision"]) {
					return report, errors.New("candidate differs from the tested final revision")
				}
				diff, e := w.diff()
				if e != nil {
					return report, e
				}
				preview, e := w.reviewDiff("final", diff)
				if e != nil {
					return report, e
				}
				u.preview("Final verified diff", "", preview)
				before, e := snapshot(w.Candidate, w.Known)
				if e != nil {
					return report, e
				}
				answer, ok := r.askChoice("Apply these verified changes to your checkout?", []string{"Apply to my checkout", "Keep in review workspace"}, false)
				if ok && answer == "Apply to my checkout" {
					after, e := snapshot(w.Candidate, w.Known)
					if e != nil {
						return report, e
					}
					if fingerprint(before) != fingerprint(after) {
						return report, errors.New("verified candidate changed during final review")
					}
					if e = w.promote(); e != nil {
						return report, e
					}
					report["applied"] = true
				}
			}
			return report, nil
		}
		if phase != "baseline" && phase != "review" && phase != "lock" && phase != "report" {
			return report, errors.New("invalid backend stage")
		}
		calls := list(response["content"])
		if len(calls) > 16 || str(response["state"]) == "" {
			return report, errors.New("invalid backend tool response")
		}
		if len(calls) == 0 {
			body = object{"requestId": uuid(), "state": response["state"], "results": []object{}}
			continue
		}
		parallelReads := len(calls) > 1
		if len(calls) > 1 {
			for _, c := range calls {
				if str(obj(c)["name"]) != "read_file" {
					parallelReads = false
				}
			}
		}
		results := make([]object, len(calls))
		var group sync.WaitGroup
		for i, raw := range calls {
			call := obj(raw)
			if turn >= o.MaxTurns && phase != "report" {
				results[i] = object{"toolUseId": str(call["id"]), "value": object{"needsReview": true, "reason": "Model turn budget reached before verification completed"}}
				continue
			}
			if !parallelReads {
				results[i] = r.tool(ctx, phase, call)
				continue
			}
			group.Add(1)
			go func(index int, item object) {
				defer group.Done()
				results[index] = r.tool(ctx, phase, item)
			}(i, call)
		}
		group.Wait()
		body = object{"requestId": uuid(), "state": response["state"], "results": results}
	}
	return report, errors.New("reporting agent exceeded its turn limit")
}
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
