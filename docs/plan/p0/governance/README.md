# P0 repository governance snapshot

- Status: PASS
- Observed at: 2026-08-10T06:22:53.516Z
- Repository: `hxp0618/cloud-agents`
- Default branch: `main@49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- Candidate: `codex/cloud-agents-platform-p0@0750f9cea5d069bbd00e77be1abd8f977da7c4b9`
- Remote candidate before this evidence commit: `0750f9cea5d069bbd00e77be1abd8f977da7c4b9`

| Check                                  | Result |
| -------------------------------------- | ------ |
| `mainStatusCheckStrict`                | PASS   |
| `mainAdminEnforced`                    | PASS   |
| `mainForcePushDisabled`                | PASS   |
| `mainDeletionDisabled`                 | PASS   |
| `rcTagRewriteProtected`                | PASS   |
| `vulnerabilityAlertsEnabled`           | PASS   |
| `privateVulnerabilityReportingEnabled` | PASS   |
| `secretScanningEnabled`                | PASS   |
| `pushProtectionEnabled`                | PASS   |
| `candidateBranchPushed`                | PASS   |
| `candidateCodeownersPresent`           | PASS   |
| `workflowActionsPinned`                | PASS   |
| `workflowRunnerNotLatest`              | PASS   |

## Boundaries

- Default-branch CODEOWNERS is currently absent; the candidate adds it for separate review.
- GitHub repository-level Actions policy does not force SHA pinning; this candidate pins every current workflow action in source.
- This evidence does not claim the candidate was merged, a release was published, or supply-chain attestation was completed.
