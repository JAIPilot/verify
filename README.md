<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="88" height="88">
</p>

# JAIPilot Verify

Turn a code change or review comment into an executable test. Verify runs the test on the base and changed revisions, then shows what happened.

<p align="center">
  <img src="assets/verify-petclinic-story.gif" alt="Verify demo on Spring Petclinic" width="1000">
</p>

*Spring Petclinic recording from an earlier interface. The current command has a simpler terminal flow.*

## Run

From a Git repository:

```sh
verify
verify --finding "The retry can charge the same payment twice"
```

Verify generates a focused test and runs it in temporary Git worktrees. Your checkout stays unchanged unless you choose to add the test. The result shows the test diff, command, both test runs, and a verdict. For example, `introduced_regression` means the test passed on the base and failed on your change. Use `verify check --json --non-interactive` in automation.

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

## Data and safety

Selected code, diffs, review findings, and test output are sent to the JAIPilot service for test generation. Verify does not send a repository archive. Test commands run locally in temporary worktrees, which are not an operating-system sandbox. Review the generated assertion and test result before relying on a verdict.

[How it works](docs/how-it-works.md) · [Contributing](CONTRIBUTING.md) · [Report an issue](https://github.com/JAIPilot/verify/issues) · [MIT License](LICENSE)
