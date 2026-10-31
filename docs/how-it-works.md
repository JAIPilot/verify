# How the earlier `verify start` workflow works

Verify is an open source local runner plus prompts. It starts a supported local coding agent in isolated Git worktrees. It does not call a JAIPilot service, require a JAIPilot login, or charge JAIPilot credits.

The [Spring Petclinic recording](../assets/verify-petclinic-story.gif) shows the earlier hosted multi-stage workflow; its [notes](../assets/demo/README.md) describe the captured run. The `verify start` terminal screens differ from that recording.

`verify start` checks GitHub's latest release before verification. A newer release is downloaded, checked against its published SHA-256 checksum, checked for the expected binary version, and installed in the same directory. The current invocation continues with the already loaded version; the next launch uses the update. Windows schedules executable replacement for after the current process exits. If GitHub is unreachable, verification continues. Run `verify update` for an explicit update check. Bare `verify` starts the managed check described in the main README.

## Workflow

| Stage | Agent task | Verify checks |
| --- | --- | --- |
| Baseline | Inspect the old code and add new tests for affected existing behavior. | Only new test files were added. The proposed characterization tests and full baseline suite pass when Verify reruns them. |
| Replay | Run the candidate's existing suite, copy the baseline tests unchanged, and replay them. | Verify records actual commands, exit codes, and output. A replay failure becomes review evidence. |
| Review | Inspect the full change, ask about unresolved intent, and edit the isolated candidate when needed. | An intent question stops editing until you answer. Verify shows the complete candidate diff and asks for acceptance. |
| Final tests | Add tests for accepted new behavior without changing production or build files. | Verify runs the full suite and optional native coverage on the final candidate revision. It shows the final tested diff before applying it. |

The editable prompts live in `internal/verify/prompts/` and are embedded in the CLI at build time. The coding agent writes a small JSON stage result containing its summary and proposed test commands. Verify runs those commands itself and records their actual exit codes; it does not accept a model-written claim that tests passed. The agent's project-specific judgment remains fallible.

The default baseline is the merge-base of your current HEAD with `origin/HEAD`, `origin/main`, `main`, `origin/master`, or `master`, in that order. Use `--base` to choose another ref. Committed branch changes, staged changes, unstaged changes, and nonignored untracked files are included.

## Commands and reports

~~~sh
verify start --agent codex --repo /path/to/project --base origin/main
verify start --json --report ./verify-report.json
verify start --non-interactive --json
~~~

`--non-interactive` stops with `needs_review` when the agent asks about intent or the candidate needs approval. It never supplies consent. The run does not yet resume automatically; its worktrees and logs remain available for inspection.

| Exit | Meaning |
| --- | --- |
| 0 | `good_to_go`: required baseline and final tests passed, and the candidate was approved. |
| 1 | `not_ready` or `needs_review`: a test failed, a blocker remains, or input is needed. |
| 2 | Setup, agent execution, cancellation, or report error. |

Private run artifacts are saved under your OS cache directory at `jaipilot/verify/<run-id>/`. They include `report.json`, `report.md`, prompts, agent logs, and both worktrees. The report records whether the final diff was applied. Verify checks that the original checkout has not changed before applying the candidate and preserves its staging area.

Verify prints update, workspace, stage, and test status. The coding agent's transcript stays in the private diagnostic log; it is not printed to the terminal. Test output is captured in the report. Status messages go to stderr, so `--json` keeps stdout available for the final structured report. The agent supplies test commands but does not run them inside its sandbox; Verify runs them directly to capture reliable results.

## Coverage

If the agent finds a configured coverage command, Verify deletes any old generated report, runs the command, and parses the fresh native report. Supported formats are Istanbul JSON, LCOV, JaCoCo XML, and Cobertura XML. The report records measured counters for changed source files. Missing or invalid data blocks a claimed coverage measurement; coverage is optional for the overall run.

Tests and coverage commands run with your local project toolchain. A passing suite checks only the behavior those tests exercise. Verify does not perform formal proofs or mutation testing.

## Data and permissions

`verify start` makes no JAIPilot API requests. The local coding agent may send repository files and command output to its configured service. Its existing account and usage terms apply.

The local worktrees keep the original checkout separate, but they are not an operating-system sandbox. The coding agent can read and edit files in its worktree and run commands needed for the workflow. Run Verify on repositories and toolchains you trust.

The CLI's source and prompts are MIT licensed. Agent integrations should be exercised on machines with their respective CLIs installed before a release claims live compatibility.
