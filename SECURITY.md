# Security

Do not report vulnerabilities in public issues. Use GitHub private vulnerability reporting when it is enabled, or contact the repository owner through an authenticated private channel.

Never attach credentials, captured traffic, private source, account data, or unredacted runtime logs to a report. Include the affected release-candidate digest and the smallest redacted reproduction possible.

The runtime treats the host environment as a trust boundary. New integrations should use `extendEnvironment: false`, anonymous credential descriptors, and the `CLOUD_AGENT_*` names. Portable and legacy aliases with different values are rejected. The CI secret scan covers the current tracked worktree and every reachable Git revision; only explicit synthetic test paths in `.secret-scan-allowlist.json` are excluded.

GitHub release-candidate artifacts are not npm releases and carry no GA security-support promise. A real authenticated provider test remains required before production use.
