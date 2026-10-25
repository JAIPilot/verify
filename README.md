<p align="center">
  <img src="assets/jaipilot-logo.svg" alt="JAIPilot" width="88" height="88">
</p>

<h1 align="center">Verify your code before you ship it</h1>

<p align="center">
  <strong>A testing agent that verifies your code.</strong><br>
  Tests new behavior and checks existing behavior for regressions.
</p>

## What Verify does

Run Verify after making a code change and before committing it. Verify uses your installed Codex or Claude Code CLI to:

1. Add tests for affected behavior on the revision before your change.
2. Run those tests on your changed code to reveal regressions.
3. Ask you about unclear intent, then show you the complete proposed diff for approval.
4. Add tests for the accepted new behavior, run the full suite, and report the evidence.

It works in separate Git worktrees. Your checkout stays untouched until you choose to apply the tested diff. The prompts and runner are in this repository. Verify makes no JAIPilot API requests and needs no JAIPilot account.

## Install

Install and sign in to [Codex](https://developers.openai.com/codex/cli) or [Claude Code](https://code.claude.com/docs/en/quickstart) first. You also need Git and your project's normal build and test tools.

**macOS / Linux**

~~~sh
curl -fsSL https://raw.githubusercontent.com/JAIPilot/verify/main/install.sh | sh
~~~

**Windows (PowerShell)**

~~~powershell
irm https://raw.githubusercontent.com/JAIPilot/verify/main/install.ps1 | iex
~~~

The installers download the latest release for your system and verify its SHA-256 checksum. To build from source instead, use Go 1.26.2 or later: `go build -o dist/verify ./cmd/verify`.

Verify checks for a newer GitHub release when it starts. If one is available, it verifies and installs the update automatically. That run finishes using the version already in memory; the next run uses the update. On Windows, replacement happens after Verify exits. Use `verify update` to check manually. Previously installed binaries need one installer run to gain the automatic updater.

Run Verify inside the Git repository containing your change:

~~~sh
verify
verify --agent codex
verify --agent claude --base origin/main
~~~

`--agent auto` uses Codex when installed, then Claude Code. Verify uses that CLI's existing sign-in and model settings. There is no `verify auth` command.

## What you see

Verify saves a JSON and Markdown report, the agent prompts and logs, test results, and both worktrees in a private cache directory. It shows the candidate diff before you approve the behavior and the final tested diff before you decide whether to apply it. A `good_to_go` result means the baseline tests passed before your change and the final test suite passed on the reported candidate. Old-behavior replay failures are included in the report so you can decide whether they are regressions or intentional changes.

Coverage is measured and reported when your project has a compatible native report. It is not a blanket 100% gate. A passing result cannot establish that untested behavior is correct.

The coding agent you select may send repository contents to its own provider under your account settings. Verify itself does not send data to JAIPilot. [See the detailed workflow and report format.](docs/how-it-works.md)

## Demo

<p align="center">
  <img src="assets/verify-petclinic-story.gif" alt="A real Spring Petclinic run showing baseline tests, a user intent decision, tests for the new behavior, and a final report" width="1000">
</p>

This recording shows the testing story in Spring Petclinic using the earlier hosted version of Verify. The local-agent version uses the same baseline, review, and final testing sequence; its terminal screens will differ. [Recording notes](assets/demo/README.md).

[Contributing](CONTRIBUTING.md) · [Report an issue](https://github.com/JAIPilot/verify/issues) · [MIT License](LICENSE)
