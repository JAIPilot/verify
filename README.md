<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="88" height="88">
</p>

<h1 align="center">Verify your change before you ship it</h1>

<p align="center">
  <strong>A testing agent for code that changes existing behavior.</strong><br>
  Learn what worked before. Find what changed. Resolve intent together. Lock the result with tests.
</p>

<p align="center">
  <a href="https://github.com/JAIPilot/verify/releases/latest">Download Verify</a> ·
  <a href="#get-started">Get started</a> ·
  <a href="#how-verify-works">How it works</a> ·
  <a href="https://www.jaipilot.com/">JAIPilot.com</a>
</p>

You build the feature with your coding agent. **Verify reviews the whole change**: new code, edits to existing code, and the behavior those edits might have disturbed. It characterizes the old revision, replays those tests on your candidate, asks about ambiguous changes, shows proposed repairs for approval, and checks the accepted result before you apply it.

## See the story: Codex → Verify → reviewed change

In this Spring Petclinic walkthrough, Codex adds **owner lookup by phone** and touches the existing owner search. The new lookup works, but partial last-name matching breaks. Verify catches the difference, asks whether that behavior should stay, and gives you a proposed repair to review.

<p align="center">
  <img src="assets/verify-petclinic-story.gif" alt="Illustrated Spring Petclinic walkthrough: Codex adds phone lookup; Verify characterizes existing name search, catches a regression, asks about intent, previews a repair, locks accepted behavior, and reports a decision" width="1000">
</p>

**The decisive moment is the conversation.** A failed baseline replay can mean a regression or an intentional product change. Verify asks before it rewrites the expectation.

<p align="center">
  <img src="assets/verify-review-loop.gif" alt="Illustrated Verify review loop: a baseline replay fails, the user clarifies that partial surname matching should remain, reviews the proposed edit, and proceeds to final tests" width="900">
</p>

These GIFs are an **illustrated product walkthrough**, following Verify's current stages and terminal interaction. They are not a recording of a completed Petclinic run or a claim about measured test results. [See the demo storyboard and rendering source.](assets/demo/README.md)

## How Verify works

| Step | What happens | Your part |
| --- | --- | --- |
| **1. Establish the baseline** | The testing agent adds characterization tests on the revision before your change, checks the existing behavior, then replays those frozen tests against your candidate. | See which old expectations still pass and which changed. |
| **2. Review intent and edits** | A reviewer weighs the diff and test evidence, asks when intent is unclear, and proposes edits. | Answer in your own words or choose an option; inspect and accept the proposed diff. |
| **3. Lock accepted behavior** | The original tester adds tests for the agreed behavior and reruns the suite, coverage, and mutation checks. | Review the final evidence. |
| **4. Get a decision** | A separate reporter explains the baseline, replay, decisions, blockers, and verdict. | Inspect the complete verified diff and choose whether to apply it. |

Verify uses separate Git worktrees for the baseline and candidate, so your checkout stays in place while it works. It shows proposed edits before acceptance and the complete verified diff before promotion. A passing verdict requires fresh evidence for affected files; missing coverage or surviving mutants appear as blockers. [Read the detailed workflow.](docs/how-it-works.md)

## Get started

Download the archive for your system from the [latest release](https://github.com/JAIPilot/verify/releases/latest), extract it, and put `verify` on your `PATH`. Then, inside your project:

```sh
verify auth login
verify --base main
```

Verify shares your JAIPilot sign-in. It runs builds and tests locally with your project's tools; have Git, the project toolchain, and compatible coverage and mutation tools configured. If your default branch is not `main`, use its ref with `--base`, or omit the flag to let Verify find a baseline.

```sh
verify --repo /path/to/project --base origin/main
verify --json --report ./verify-report.json
verify --non-interactive --json
```

Answer yes/no prompts with `y` or `n`. Other reviews accept a numbered choice or a comment typed directly. Noninteractive runs stop with `needs_review` when your decision is required.

## What you get

- **A tested comparison:** characterization tests from the old revision, replayed on your candidate.
- **A review you can steer:** questions about intent and previews of proposed changes.
- **A final evidence gate:** local suite, coverage, and mutation results for affected files.
- **A clear handoff:** report, verdict, complete diff, and a separate choice to apply it.

The diff, requested file contents, test results, and command output are sent to JAIPilot and its model provider; builds and tests run locally. Passing checks protect the behavior they exercise, but do not prove a change is bug-free. [Data handling, evidence formats, exit codes, and development details →](docs/how-it-works.md)

[Contributing](CONTRIBUTING.md) · [Report an issue](https://github.com/JAIPilot/verify/issues) · [MIT License](LICENSE)
