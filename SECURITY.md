# Security

Report security vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/JAIPilot/verify/security/advisories/new). Do not include credentials or private source code in public issues.

Verify sends selected code, diffs, reviewer findings, and bounded test output to the JAIPilot service for test generation. It runs proposed test commands locally with your project tools. Worktrees isolate checkout changes but do not restrict operating-system access. Review generated tests and use trusted repositories.

The updater contacts GitHub, validates the release archive against `checksums.txt`, and checks the downloaded executable's reported version before replacing the installed binary. Keep the installation directory writable only by its owner.
