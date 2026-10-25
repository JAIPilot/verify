package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func selectAgent(requested string) (string, error) {
	if requested != "auto" && requested != "codex" && requested != "claude" {
		return "", errors.New("--agent must be auto, codex, or claude")
	}
	if requested == "auto" {
		for _, name := range []string{"codex", "claude"} {
			if _, err := exec.LookPath(name); err == nil {
				return name, nil
			}
		}
		return "", errors.New("install Codex or Claude Code, then sign in with that CLI")
	}
	if _, err := exec.LookPath(requested); err != nil {
		return "", fmt.Errorf("%s is not installed or is not on PATH", requested)
	}
	return requested, nil
}

func runAgentStage(ctx context.Context, agent, root, runDir, stage, prompt string) (stageResult, error) {
	resultName := ".verify-stage-" + uuid() + ".json"
	resultPath := filepath.Join(root, resultName)
	defer os.Remove(resultPath)
	prompt += "\n\nWrite your result as a single JSON object to " + resultName + " in this workspace. " +
		"Use exactly these string fields when relevant: summary, testCommand, existingSuiteCommand, " +
		"coverageCommand, coverageReport, coverageFormat, question, blocker; options is an array of strings. " +
		"Do not put the result in your final chat message instead of the file. " +
		"Do not edit this result file after writing it.\n"
	if err := writeBytes(filepath.Join(runDir, stage+".prompt.txt"), []byte(prompt), 0600); err != nil {
		return stageResult{}, err
	}
	log, err := os.OpenFile(filepath.Join(runDir, stage+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return stageResult{}, err
	}
	defer log.Close()
	var cmd *exec.Cmd
	switch agent {
	case "codex":
		cmd = directCommand(root, "codex", "exec", "--cd", root, "--sandbox", "workspace-write", "-")
		cmd.Stdin = strings.NewReader(prompt)
	case "claude":
		cmd = directCommand(root, "claude", "-p", "--permission-mode", "acceptEdits", "--allowedTools", "Read,Edit,Write,Bash", prompt)
	default:
		return stageResult{}, fmt.Errorf("unsupported agent %q", agent)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.WaitDelay = 3 * time.Second
	u := newTerminalUI(os.Stdin, os.Stderr)
	u.activity(agent + " is working on " + stage + "…")
	if err := cmd.Start(); err != nil {
		return stageResult{}, err
	}
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err = <-done:
			goto finished
		case <-ticker.C:
			u.activity("Still working on " + stage + " (" + time.Since(started).Round(time.Second).String() + ")")
		case <-ctx.Done():
			terminate(cmd, false)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				terminate(cmd, true)
				<-done
			}
			return stageResult{}, ctx.Err()
		}
	}

finished:
	if err != nil {
		return stageResult{}, fmt.Errorf("%s failed in %s: %w (see %s)", agent, stage, err, log.Name())
	}
	result, err := parseStageResult(resultPath)
	if err != nil {
		return result, fmt.Errorf("%s: %w (see %s)", stage, err, log.Name())
	}
	label := result.Summary
	if result.Question != "" {
		label = "Needs an intent decision"
	} else if result.Blocker != "" {
		label = "Blocked: " + result.Blocker
	}
	if len(label) > 220 {
		label = label[:220] + "…"
	}
	u.note("Agent", label)
	return result, nil
}
