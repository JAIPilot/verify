# How Verify works

1. Verify finds the base revision and snapshots the Git change into two temporary worktrees.
2. It sends selected changed files, relevant context, and any reviewer finding to JAIPilot for one focused test proposal.
3. It validates the proposed test path and command, then runs the same test on the base and changed revisions with your local project tools. If the test does not build or reproduce the claim, Verify can try again using bounded test output. Each run allows up to three proposals.
4. It saves the test diff, command output, and verdict in a private report. Your checkout stays unchanged unless you choose to add the test file.

## Verdicts

| Verdict | Observed result |
| --- | --- |
| `introduced_regression` | Test passes on the base and fails on the change. |
| `changed_behavior_verified` | Test fails on the base and passes on the change. |
| `no_reproduction_found` | Test passes on both revisions. |
| `inconclusive` | The runs do not support a conclusion, including failures on both revisions or timeouts. |

A verdict describes one generated test. Review its assertion before relying on it. The command exit code reports whether Verify completed; use the JSON `verdict` when gating a change.

## Data and execution

Verify sends selected source, diffs, reviewer findings, and bounded test output to the JAIPilot service. It does not send a repository archive. Test commands run on your machine in temporary worktrees using your project toolchain. Worktrees isolate checkout changes but are not an operating-system sandbox. Reports and worktrees are saved under your OS cache directory at `jaipilot/verify/<run-id>/`.

Run `verify --help` for options, `verify check --json --non-interactive` for automation, or `verify update` to install the latest release.
