# How Verify works

This guide explains the workflow and evidence behind [Verify](../README.md). Builds and tests run locally; three Claude agents work through JAIPilot's managed backend.

If you are already signed in with `jaipilot`, Verify uses the same session. `verify auth logout` signs out that shared session. Git, a working project toolchain, and configured test, coverage, and mutation tools are required. Go 1.26.2 or later is needed when installing from source.

## Four stages, three agents

| Stage | Agent | What happens |
| --- | --- | --- |
| 1. Establish the baseline | Tester | Start at the merge-base before your changes. Add characterization tests in new files. Run tests, require 100% measured coverage and mutation score for affected existing source files, then run the candidate's full existing suite and replay the same frozen tests against your diff. Review the diff alongside passing and failing results. |
| 2. Review intent and edits | Reviewer | Ask about intended behavior, distinguish regressions from intentional changes, propose code and test edits, and get your approval. You explicitly accept the complete candidate diff before continuing. |
| 3. Lock the accepted behavior | The same tester | Keep accepted production code frozen. Add or update characterization tests for the new behavior. Require fresh passing tests, 100% measured coverage, and 100% mutation score for the final affected source files. |
| 4. Report | Separate, simpler reporter | Explain the baseline, replay results, decisions, edits, final evidence, blockers, and verdict. This agent has no shell or edit tools. |

The testing and review agents use Claude Sonnet. Reporting uses Claude Haiku with a smaller context and output task. Agent prompts, conversation routing, stage gates, auth checks, and usage accounting live in the JAIPilot backend. The CLI handles Git worktrees, local execution, native report parsing, approvals, and final promotion.

Passing stage 1 does **not** skip review. Failing replay tests can reveal an intentional behavior change; the reviewer must resolve that intent before the new behavior is locked. New files have no baseline behavior to characterize, but are included in the final testing scope.

## Git scope and approvals

Verify includes committed changes on the current branch, staged changes, unstaged changes, and nonignored untracked files. The default baseline is the merge-base with `origin/HEAD`, `origin/main`, `main`, `origin/master`, or `master`, in that order. Use `--base` to select another ref.

Two detached worktrees hold the baseline and candidate. Your original files and staging area stay in place during verification. Stage 1 adds new test files without changing existing tracked files. Stage 2 previews every proposed file edit and asks for acceptance. Stage 3 can only change tests.

After a successful final report, Verify shows the complete verified diff and offers to apply it. Promotion checks that both the original checkout and the tested candidate are unchanged. It preserves the staging area and creates no commits. Declining promotion keeps the verified result in its candidate worktree.

Worktrees separate file changes; they are **not an operating-system sandbox**. Project builds and model-requested shell commands run with your local permissions. Run Verify on repositories you trust.

## What the evidence means

The target applies conservatively to each entire affected source file, including renamed, added, and deleted files where that version exists. Tests are recognized by conventional test directories and filenames. Non-source changes are reviewed but do not receive invented coverage numbers.

Native report adapters:

| Evidence | Supported formats |
| --- | --- |
| Coverage | Istanbul JSON, LCOV, JaCoCo XML, Cobertura XML |
| Mutation | Stryker JSON, PIT XML |

The agent selects commands appropriate to your project. Existing project instructions can document those commands and report paths. A successful measurement must regenerate its report; stale report files are removed first. Every scoped file must have nonempty primary counters. All measured lines or statements and branches must be covered; all valid generated mutants must be killed. Surviving, ignored, uncovered, errored, and timed-out mutants do not count as killed. Noncompiling/nonviable mutants are excluded and recorded in the evidence.

Some formats do not contain branch counters. That limitation is reported; Verify does not infer branch coverage. A project without compatible tooling gets a blocker report. Unsupported languages, equivalent surviving mutants, code without viable mutants, and unreachable code can prevent a 100% result. There is no automatic lowering of thresholds or acceptance of a model-written score. Passing tests and mutation checks protect the behavior exercised; they do not prove that a change is bug-free.

For a Node project, for example, configure [c8](https://github.com/bcoe/c8) to write Istanbul `coverage/coverage-final.json` and [Stryker](https://stryker-mutator.io/docs/stryker-js/configuration/) to write `reports/mutation/mutation.json`. For Java, configure JaCoCo and PIT. Install/configure tooling before verification: baseline production files and build configuration are immutable.

## Reports and exit codes

```sh
verify --repo /path/to/project --base origin/main
verify --json --report ./verify-report.json
verify --non-interactive --json
verify --max-turns 100
```

`--non-interactive` produces `needs_review` when an answer or approval is required. It never supplies consent. `--max-turns` limits model turns, then sends unfinished work to the reporting agent. Each local command has a model-selected timeout, capped at two hours. Press Ctrl-C to cancel.

| Exit | Meaning |
| --- | --- |
| `0` | `good_to_go`: all four stages completed and evidence gates passed |
| `1` | `not_ready` or `needs_review`: verification reported blockers or needs human input |
| `2` | Setup, authentication, service, cancellation, or execution error |

Private run artifacts are saved under your OS cache directory at `jaipilot/verify/<run-id>/`: `report.json`, `report.md`, raw evidence, and both worktrees. The JSON includes exact commands, outputs, parsed counters, decisions, and whether the changes were applied. Service failures also produce a local incomplete report. Runs are retained for inspection; automatic restart/resume is not yet supported. Remove retained worktrees with `git worktree remove --force <path>`, then remove their run directory.

## Authentication and data

`verify auth login|status|logout` uses JAIPilot's browser login and refresh endpoints. Session location is the same as `jaipilot`: `JAIPILOT_CONFIG_DIR/jaipilot/session.json`, or the existing JAIPilot default under `XDG_CONFIG_HOME`, Windows `APPDATA`, or `~/.config`. Session files and run artifacts use private permissions.

The diff, requested file contents, test results, and command output are sent to JAIPilot and its Claude provider. Authentication tokens stay out of repository files and reports. Continuation state is encrypted and authenticated by the backend and bound to the signed-in account. Requests use the shared JAIPilot subscription/trial and usage accounting, with idempotent model turns. Open-source licensing covers this CLI; the hosted service has its own access requirements.

For local backend development, `JAIPILOT_VERIFY_ENDPOINT` overrides the managed endpoint. Point it only at a service you trust with your JAIPilot bearer token. The private backend function is `invoke-verify-agent` in `jaipilot-functions`; no backend credentials or provider API keys belong in this repository.

## Development

```sh
go test -race ./...
go vet ./...
go build -o dist/verify ./cmd/verify
```

Tests exercise real Git snapshots, a real Node characterization regression, approval rejection, protected source edits, promotion races, native report parsing, and noninteractive reporting. Node.js is needed for the characterization integration test. Backend stage-machine, authenticated-state, billing, and HTTP tests live with the backend function.

The auth and process helpers are derived from [JAIPilot](https://github.com/JAIPilot/jaipilot), also MIT licensed.

[MIT License](LICENSE)
