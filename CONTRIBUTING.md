# Contributing

Open an issue for behavior changes, then submit a focused pull request with a clear example and relevant verification. Run `go test -race ./...` and `go vet ./...`. Use `gofmt` for Go files. Tests that execute JavaScript require Node.js.

Keep agent prompts and workflow decisions in the backend. Keep local Git operations, command execution, native evidence parsing, approvals, and report persistence in the CLI. Preserve the shared JAIPilot login contract. A backend change should include tests in the private backend repository and a matching public protocol change when needed.

Never commit account sessions, bearer tokens, provider keys, private repository contents, or real user reports. Use disposable fixture repositories for integration tests. Evidence must describe actual commands and native report counters; missing evidence must fail closed.
