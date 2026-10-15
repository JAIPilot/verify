package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxOutput = 80000

type CommandResult struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	TimedOut   bool   `json:"timedOut"`
	DurationMS int64  `json:"durationMs"`
}
type tailWriter struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, p...)
	if len(w.data) > maxOutput {
		w.data = append([]byte{}, w.data[len(w.data)-maxOutput:]...)
		w.truncated = true
	}
	return len(p), nil
}

type Execute func(context.Context, string, object) (CommandResult, error)

func validateCommand(in object) (string, int, error) {
	command := str(in["command"])
	seconds := num(in["timeoutSeconds"])
	n, ok := in["timeoutSeconds"].(float64)
	if !ok {
		if i, yes := in["timeoutSeconds"].(int); yes {
			n = float64(i)
			ok = true
		}
	}
	if strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) || !ok || n != float64(seconds) || seconds < 1 || seconds > 7200 {
		return "", 0, errors.New("Invalid local command request")
	}
	return command, seconds, nil
}
func runCommand(ctx context.Context, root string, in object) (CommandResult, error) {
	command, seconds, err := validateCommand(in)
	if err != nil {
		return CommandResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	purpose := str(in["purpose"])
	if purpose == "" {
		purpose = "task"
	}
	fmt.Fprintf(os.Stderr, "JAIPilot: %s…\n", purpose)
	started := time.Now()
	cmd := shellCommand(root, command)
	out, stderr := &tailWriter{}, &tailWriter{}
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Start(); err != nil {
		return CommandResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	timedOut := false
	cancelled := false
	select {
	case err = <-done:
	case <-ctx.Done():
		cancelled = true
	case <-timer.C:
		timedOut = true
	}
	if cancelled || timedOut {
		terminate(cmd, false)
		escalation := time.NewTimer(2 * time.Second)
		select {
		case err = <-done:
			if !escalation.Stop() {
				<-escalation.C
			}
		case <-escalation.C:
			terminate(cmd, true)
			err = <-done
		}
		terminate(cmd, true)
	}
	if cancelled {
		return CommandResult{}, ctx.Err()
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
			if code < 0 {
				code = 1
			}
		} else {
			return CommandResult{}, err
		}
	}
	if timedOut {
		code = 124
	}
	output := string(out.data) + "\n" + string(stderr.data)
	output = strings.TrimSpace(output)
	truncated := out.truncated || stderr.truncated || len(output) > maxOutput
	if len(output) > maxOutput {
		output = "\n… earlier command output omitted …\n" + output[len(output)-maxOutput:]
	}
	return CommandResult{command, code, output, truncated, timedOut, time.Since(started).Milliseconds()}, nil
}
