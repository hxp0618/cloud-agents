# Security

Do not report vulnerabilities in public issues. Use GitHub private vulnerability reporting when it is enabled, or contact the repository owner through an authenticated private channel.

Never attach credentials, captured traffic, private source, account data, or unredacted runtime logs to a report. Include the affected release-candidate digest and the smallest redacted reproduction possible.

The [foundation security acceptance](docs/plan/cloud-agents-platform/05-gates-and-acceptance.md) covers tenant authorization, workspace ownership and deletion, single-writer fencing, access tokens, actual network enforcement, runtime isolation, and customer-node enrollment/revocation. A trusted single-tenant Docker profile is not proof of hostile multi-tenant isolation. Non-root, capability drops, and read-only rootfs are useful controls, not substitutes for a qualified isolation runtime. A customer-controlled host remains trusted for its own workload; enrollment does not conceal workload data from its host administrator.

Persistent Workspace/Volume deletion is distinct from Sandbox/Lease cleanup. Preserve the Admin Cleanup resource-name and generation confirmation; it is a product interaction requirement, not confirmation for every development action.

The Agent runtime treats the host environment as a trust boundary. New integrations should use `extendEnvironment: false`, anonymous credential descriptors, and the `CLOUD_AGENT_*` names. Portable and legacy aliases with different values are rejected. The CI secret scan covers the current tracked worktree and every reachable Git revision; only explicit synthetic test paths in `.secret-scan-allowlist.json` are excluded.

GitHub release-candidate artifacts are not npm releases and carry no GA security-support promise. A real authenticated provider test remains required before production use of Agent-enabled features. A foundation-only deployment instead requires its no-Agent lifecycle/access/isolation/recovery acceptance; neither test path grants production deployment or publication approval.
