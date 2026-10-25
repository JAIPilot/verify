package verify

import (
	_ "embed"
	"strings"
)

//go:embed prompts/common.txt
var promptCommon string

//go:embed prompts/baseline.txt
var promptBaselineText string

//go:embed prompts/review.txt
var promptReviewText string

//go:embed prompts/lock.txt
var promptLockText string

func promptBaseline(w *workspace) string {
	return promptCommon + strings.NewReplacer("{{BASE}}", w.Base, "{{SCOPE}}", jsonText(w.BaselineScope)).Replace(promptBaselineText)
}

func promptReview(_ *workspace, report object, answers []object) string {
	evidence := object{
		"baselineTests":          promptEvidence(report["baselineTests"], 500),
		"baselineSuite":          promptEvidence(report["baselineSuite"], 500),
		"candidateExistingSuite": promptEvidence(report["candidateExistingSuite"], 4000),
		"replay":                 promptEvidence(report["replay"], 4000),
		"frozenTests":            report["frozenTests"],
	}
	return promptCommon + strings.NewReplacer("{{EVIDENCE}}", jsonText(evidence), "{{ANSWERS}}", jsonText(answers)).Replace(promptReviewText)
}

func promptEvidence(value any, maxOutput int) object {
	result, ok := value.(CommandResult)
	if !ok {
		return object{}
	}
	output := result.Output
	if len(output) > maxOutput {
		output = "… earlier output omitted …\n" + output[len(output)-maxOutput:]
	}
	return object{"command": result.Command, "exitCode": result.ExitCode, "output": output, "timedOut": result.TimedOut}
}

func promptLock(w *workspace, report object) string {
	return promptCommon + strings.NewReplacer("{{SCOPE}}", jsonText(w.FinalScope), "{{DECISIONS}}", jsonText(report["decisions"])).Replace(promptLockText)
}
