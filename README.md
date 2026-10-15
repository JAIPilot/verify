<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="80" height="80">
</p>

<h1 align="center">Verify</h1>

<p align="center"><strong>Verify your code. Catch and fix bugs quickly.</strong></p>

<p align="center">
  <a href="#get-started">Get started</a> ·
  <a href="#how-verify-works">How it works</a> ·
  <a href="https://www.jaipilot.com/">JAIPilot.com</a>
</p>

Verify is an open-source CLI from [JAIPilot](https://www.jaipilot.com/) that checks your Git changes against the behavior of the code they replace. It builds a tested baseline, reviews the changes with you, locks the accepted behavior with tests, and gives you a clear go/no-go report.

## Get started

Download the archive for your system from the [v0.1.0 release](https://github.com/JAIPilot/verify/releases/tag/v0.1.0), extract it, and put `verify` on your PATH. Then run:

```sh
verify auth login
cd your-project
verify --base main
```

Verify shares your JAIPilot sign-in. It runs your project's tests locally, so Git, your project toolchain, and configured test, coverage, and mutation tools are needed. Your local changes stay in place while Verify works in separate Git worktrees.

## How Verify works

1. **Establish the baseline.** Characterize the existing code, require full measured coverage and mutation testing for affected files, then run the existing suite and the new tests against your changes.
2. **Review and fix.** A review agent examines the diff and test results, asks about your intent, and proposes edits for your approval.
3. **Lock the behavior.** The original testing agent adds tests for the accepted behavior and reruns tests, coverage, and mutation checks.
4. **Get the report.** A separate reporting agent explains the evidence, decisions, blockers, and final verdict.

Verify shows you the complete tested diff before offering to apply it. A passing report requires fresh evidence for the affected files; missing coverage or surviving mutants appear as blockers.
Questions and edit reviews accept a numbered choice or a comment typed directly.

```sh
verify --repo /path/to/project --base origin/main
verify --json --report ./verify-report.json
```

[How it works in detail](docs/how-it-works.md) · [Contributing](CONTRIBUTING.md) · [MIT License](LICENSE)
