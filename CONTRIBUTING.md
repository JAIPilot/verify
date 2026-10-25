# Contributing

Open an issue for behavior changes, then submit a focused pull request with a clear example and relevant verification. Run `go test -race ./...` and `go vet ./...`. Use `gofmt` for Go files. Tests that execute JavaScript require Node.js.

Keep agent prompts readable and provider-neutral. The CLI owns local Git worktrees, actual test execution, native evidence parsing, approvals, and report persistence. Changes to a prompt should include a fixture showing the agent's expected stage result and the local runner's checks.

Never commit account sessions, provider keys, private repository contents, or real user reports. Use disposable fixture repositories for integration tests. Evidence must describe actual commands and native report counters; do not turn missing evidence into a passing measurement.
