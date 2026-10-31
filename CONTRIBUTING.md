# Contributing

Open an issue for behavior changes, then submit a focused pull request with a clear example and relevant verification. Run `go test -race ./...` and `go vet ./...`. Use `gofmt` for Go files. Tests that execute JavaScript require Node.js.

The CLI owns local Git worktrees, test execution, approvals, and report persistence. Managed test generation and credit accounting live in the JAIPilot functions repository. Changes to a prompt or proposal rule should include a fixture showing how the CLI validates and executes the result.

Never commit account sessions, provider keys, private repository contents, or real user reports. Use disposable fixture repositories for integration tests. Evidence must describe actual commands and native report counters; do not turn missing evidence into a passing measurement.
