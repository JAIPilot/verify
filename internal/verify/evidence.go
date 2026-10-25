package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func directEvidenceCommand(command string) error {
	if strings.TrimSpace(command) == "" || strings.ContainsAny(command, "|;&\n\r<>") {
		return errors.New("evidence command must be one direct command without pipes, redirection, or chaining")
	}
	return nil
}

func runEvidence(ctx context.Context, root, command, purpose string) (CommandResult, error) {
	if err := directEvidenceCommand(command); err != nil {
		return CommandResult{}, err
	}
	return execute(ctx, root, object{"command": command, "timeoutSeconds": 7200, "purpose": purpose})
}

func runCoverage(ctx context.Context, w *workspace, stage stageResult) (object, error) {
	if stage.CoverageReport == "" || stage.CoverageFormat == "" {
		return nil, errors.New("coverage command needs a report path and format")
	}
	path, err := safePath(w.Candidate, stage.CoverageReport)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(w.Candidate, path)
	if err != nil {
		return nil, err
	}
	current, err := snapshot(w.Candidate, w.Known)
	if err != nil {
		return nil, err
	}
	if _, exists := current[filepath.ToSlash(rel)]; exists {
		return nil, errors.New("coverage report path is a source or tracked file")
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	result, err := runEvidence(ctx, w.Candidate, stage.CoverageCommand, "Final coverage")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("coverage command exited %d: %s", result.ExitCode, result.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fresh coverage report missing: %w", err)
	}
	if len(data) > 32<<20 {
		return nil, errors.New("coverage report exceeds 32 MiB")
	}
	format := stage.CoverageFormat
	switch format {
	case "jacoco-xml":
		format = "jacoco"
	case "istanbul-json":
		format = "istanbul"
	case "cobertura-xml":
		format = "cobertura"
	}
	measured, err := parseMetrics(data, format, w.Candidate, w.FinalScope)
	if err != nil {
		return nil, err
	}
	artifact := filepath.Join(w.RunDir, "evidence", uuid()+"-"+filepath.Base(path))
	if err = writeBytes(artifact, data, 0600); err != nil {
		return nil, err
	}
	return object{"command": result, "metrics": measured, "artifact": artifact}, nil
}

var execute = func(ctx context.Context, root string, in object) (CommandResult, error) {
	return runCommand(ctx, root, in)
}
