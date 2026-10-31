<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="88" height="88">
</p>

# JAIPilot Verify

Turn a code change or review comment into an executable test. Verify runs the test on the base and changed revisions, then shows what happened.

## Run

From a Git repository:

```sh
verify
verify --finding "The retry can charge the same payment twice"
```

Verify generates a focused test and runs it in temporary Git worktrees. Your checkout stays unchanged unless you choose to add the test. The result includes both test runs, the generated test diff, and a verdict. Use `verify check --json --non-interactive` in automation.

Verify uses your JAIPilot sign-in and shared credits for test generation. Local test execution uses your existing project tools and consumes no JAIPilot credits.

## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/JAIPilot/verify/main/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/JAIPilot/verify/main/install.ps1 | iex
```

You need Git and your project's test tools. The first run opens browser sign-in if needed. Run `verify --help` for options.

## Deeper review

Run `verify start` for the earlier multi-stage workflow. It adds baseline tests, asks about unclear intent, can edit the candidate source, runs the full suite and optional coverage, and asks before applying the tested diff. It uses a signed-in local coding agent and does not use JAIPilot credits. See [how it works](docs/how-it-works.md).

<p align="center">
  <img src="assets/verify-petclinic-story.gif" alt="Earlier Verify workflow on Spring Petclinic" width="1000">
</p>

The [Spring Petclinic recording](assets/demo/README.md) shows an earlier version of this workflow; the current terminal screens differ.

## Data and safety

Selected code, diffs, review findings, and test output are sent to the JAIPilot service for test generation. Verify does not send a repository archive. Test commands run locally in temporary worktrees, which are not an operating-system sandbox. Review the generated assertion and test result before relying on a verdict.

[Contributing](CONTRIBUTING.md) · [Report an issue](https://github.com/JAIPilot/verify/issues) · [MIT License](LICENSE)
