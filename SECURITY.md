# Security

Report security vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/JAIPilot/verify/security/advisories/new). Do not include credentials or private source code in public issues.

Verify runs project builds and agent-requested commands locally. Its worktrees isolate edits but do not restrict operating-system access. Review proposed changes and use trusted repositories. The backend must enforce authentication, access, usage accounting, stage transitions, and account-bound continuation state.
