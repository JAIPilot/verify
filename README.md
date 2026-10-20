<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="88" height="88">
</p>

<h1 align="center">Verify your change before you ship it</h1>

<p align="center">
  <strong>A testing agent for code changes:<br>
  lock existing behavior, review intent, and verify the result.</strong>
</p>

<p align="center">
  <a href="https://github.com/JAIPilot/verify/releases/latest">Download Verify</a> ·
  <a href="#get-started">Get started</a> ·
  <a href="#how-verify-works">How it works</a> ·
  <a href="https://www.jaipilot.com/">JAIPilot.com</a>
</p>

## What Verify does

1. **Make a change with your coding agent.** Verify examines your new code and the existing code you edited.
2. **Check for regressions.** Verify tests affected behavior before your change, then runs those tests on the changed code to spot regressions.
3. **Decide what should happen.** Verify asks about unclear behavior and shows proposed code or test edits for your approval.
4. **Test the new code.** Verify adds tests for the agreed behavior, runs the project suite and coverage checks, and reports the result before you apply the verified diff.

In the Petclinic demo below, a coding agent adds new pet type aliases. Verify tests those aliases and checks that existing pet names and errors still work.

## Watch one real run: coding agent → Verify → `good_to_go`

In [Spring Framework Petclinic](https://github.com/spring-petclinic/spring-framework-petclinic), a coding agent added `kitty` → `cat` and `puppy` → `dog` in a new resolver and edited the existing formatter. This captioned GIF follows the actual local run from that change to Verify's final report.

<p align="center">
  <img src="assets/verify-petclinic-story.gif" alt="One captioned recording: a coding agent adds pet type aliases, Verify checks for regressions, the user decides an edge case, Verify tests the new code, and the run reports good_to_go" width="1000">
</p>

Verify found an untested missing-target case. During review, we confirmed that it should preserve the original error; Verify added tests for that rule and the new resolver. The full Maven suite and coverage checks passed, and Verify reported `good_to_go`. The GIF uses selected frames from real terminal recordings with explanatory captions. [See the run and recording notes.](assets/demo/README.md)

## How Verify works

| Step | What happens | Your part |
| --- | --- | --- |
| **1. Establish the baseline** | The testing agent adds characterization tests on the revision before your change, checks the existing behavior, then replays those frozen tests against your candidate. | See which old expectations still pass and which changed. |
| **2. Review intent and edits** | A reviewer weighs the diff and test evidence, asks when intent is unclear, and proposes edits. | Answer in your own words or choose an option; inspect and accept the proposed diff. |
| **3. Lock accepted behavior** | The original tester adds tests for the agreed behavior and reruns the suite and coverage checks. | Review the final evidence. |
| **4. Get a decision** | A separate reporter explains the baseline, replay, decisions, blockers, and verdict. | Inspect the complete verified diff and choose whether to apply it. |

Verify uses separate Git worktrees for the baseline and candidate, so your checkout stays in place while it works. It shows proposed edits before acceptance and the complete verified diff before promotion. A passing verdict requires fresh test and coverage evidence for affected files. [Read the detailed workflow.](docs/how-it-works.md)

## Get started

Download the archive for your system from the [latest release](https://github.com/JAIPilot/verify/releases/latest), extract it, and put `verify` on your `PATH`. Then, inside your project:

```sh
verify auth login
verify --base main
```

Verify shares your JAIPilot sign-in. It runs builds and tests locally with your project's tools; have Git, the project toolchain, and compatible coverage tooling configured. If your default branch is not `main`, use its ref with `--base`, or omit the flag to let Verify find a baseline.

```sh
verify --repo /path/to/project --base origin/main
verify --json --report ./verify-report.json
verify --non-interactive --json
```

Answer yes/no prompts with `y` or `n`. Other reviews accept a numbered choice or a comment typed directly. Noninteractive runs stop with `needs_review` when your decision is required.

## What you get

- **A tested comparison:** characterization tests from the old revision, replayed on your candidate.
- **A review you can steer:** questions about intent and previews of proposed changes.
- **A final evidence gate:** local tests and coverage results for affected files.
- **A clear handoff:** report, verdict, complete diff, and a separate choice to apply it.

The diff, requested file contents, test results, and command output are sent to JAIPilot and its model provider; builds and tests run locally. Passing checks protect the behavior they exercise, but do not prove a change is bug-free. [Data handling, evidence formats, exit codes, and development details →](docs/how-it-works.md)

[Contributing](CONTRIBUTING.md) · [Report an issue](https://github.com/JAIPilot/verify/issues) · [MIT License](LICENSE)
