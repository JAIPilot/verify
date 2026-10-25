# Security

Report security vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/JAIPilot/verify/security/advisories/new). Do not include credentials or private source code in public issues.

Verify runs your installed coding agent and project builds locally. Its worktrees isolate edits but do not restrict operating-system access. Review proposed changes and use trusted repositories. The selected coding agent may send code and output to its configured provider; Verify does not use a JAIPilot backend.

The updater contacts GitHub, validates the release archive against `checksums.txt`, and checks the downloaded executable's reported version before replacing the installed binary. Keep the installation directory writable only by its owner.
