# M5 OrbStack browser E2E

Date: 2026-09-03

Status: passed.

## Build under test

- Platform candidate: `0.0.0-m5.d23977c`, source commit `d23977c1bd7ea030584f66313822602c9061f686`.
- User Web source: `a827183e0df8db74568a861712a67689d5bc0dd3`.
- Follow-up Docker cleanup fix: `a6dd4959fb78c3eec982e9e631995572929e2a57`; candidate `0.0.0-m5.a6dd495` built successfully and passed the packaged OrbStack Compose smoke after the browser run.
- Release manifests recorded `sourceDirty: true` only because unrelated pre-existing worktree changes were preserved; no dirty file was included in these commits.
- Browser: Codex in-app browser against the Vite same-origin proxy and a local OrbStack Docker target.

## Target, Lease, and zero-downtime upgrade

- Tenant: `tenant-compose-smoke`.
- Project: `project-2fde604e9ef099aad6125e5a2181196c`.
- Target: `docker-compose-target`.
- Lease: `lease-compose-target`.
- Upgrade observation:

  ```text
  generation 1 ready       | generation 1 running
  generation 2 provisioning| generation 1 running
  generation 2 provisioning| generation 1 and 2 running
  generation 2 ready       | generation 1 and 2 running
  generation 2 ready       | generation 2 running
  ```

- Generation 2 reused the exact generation 1 workspace volume. Generation 1 was removed only after generation 2 became Ready.

## Real Codex browser run

- Session: `session-web-m5-codex`, generation 2.
- Turn: `turn-5906a9eb-a7a5-41a8-bdff-ae36120a70f7`.
- Execution: `execution-339f73f4-e15d-43e1-99eb-cc4532d03612`.
- Real provider event identified Codex and model `gpt-5.3-codex-spark`.
- File-change Approval `codex:generation-2:0` and command Approval `codex:generation-2:1` were accepted in the browser; the same Execution reached `succeeded`.
- Artifact `.cloud-agents-acceptance/web-m5-codex.txt` was downloaded through the browser. Message index `23` independently returned the exact bytes `cloud-agents Web Codex real E2E\n`.

## Real Claude Code browser run

- Session: `session-web-m5-claude`, generation 2.
- Turn: `turn-8b7e6313-4896-4dc0-91c7-b26d2aad888a`.
- Execution: `execution-f320a772-fef1-41e5-9a42-18bbfb59b1eb`.
- The real `claudeAgent` provider path requested Write approval `claude:generation-2:approval:call_FvcZJDuN1sxjEvXdIPX1vJeX` in the browser; after acceptance the same Execution reached `succeeded`.
- Evidence boundary: this run emitted no usable native Diff and therefore no ArtifactCandidate. The runtime warning was `Claude Write completed without a usable native Diff`; the Codex run above supplies the required real Artifact evidence.

## Interaction, cancellation, and recovery

- User Input Execution: `execution-ac64065a-1f52-45ed-9f8b-b4d83a52d8a6`.
- User Input request: `codex:generation-2:0`, generation 2. The browser selected `Staging` for “Which environment should I use for this run?” and the same Execution reached `succeeded`.
- Cancel Execution: `execution-9a2361ea-7e56-42b5-9403-0117cc03ea86`; terminal state `cancelled`, event `turn.cancel`.
- Interrupt Execution: `execution-506c5b65-ae43-4343-8280-cdde1dd96f66`; unified terminal state `cancelled`, event `turn.interrupt`.
- After a page reload the bearer token field was empty. Reconnecting with the memory-only token restored the generation 2 Codex Session and cancelled Execution from server state; prompt plaintext and retry state were not persisted.
- A later Control Plane container replacement preserved the same database state and continuously running generation 2 target Worker. A fresh token then re-read all five Execution states and the Artifact bytes from the server.

## Termination and cleanup

- The browser issued Terminate for generation 2. The first actuator call returned 502 after the original host-side test mTLS forwarder had exited; the durable state was retained as generation 3, `desiredPhase=terminated`, `observedPhase=terminating`, and `cleanupPhase=pending`.
- After restoring the same target endpoint, replaying the exact idempotent termination completed generation 3 as `desiredPhase=terminated`, `observedPhase=terminated`, and `cleanupPhase=complete`. A browser refresh displayed the Lease as `TERMINATED` and the Worker as `Offline`.
- Browser Target Cleanup returned 200. The Lease owned zero Worker containers afterward.
- The older `d23977c` candidate left its reused anonymous workspace volume after removing the last Worker. It had zero container references and was deleted only after explicit user confirmation; the volume was then absent.
- The fixed `a6dd495` candidate subsequently passed the packaged Compose smoke against OrbStack. Managed workspace volumes were absent both before and after the run, and its self-owned Compose containers, Worker containers, and Compose volumes were all zero after teardown.

## Verification

- User Web typecheck passed.
- Vitest: 3 files, 21 tests passed.
- Production build passed.
- Repository lint passed with warnings denied.
- Docker actuator and Control Plane server focused tests passed.
- Current-state recheck: Codex, Claude Code, and User Input Executions remained `succeeded`; Cancel and Interrupt remained `cancelled`; event history retained `turn.cancel` and `turn.interrupt`.
- Final cleanup evidence: Lease `terminated` with `cleanupPhase=complete`, browser Worker `Offline`, browser Target Cleanup 200, no owned Worker container, no workspace volume, and no acceptance Compose container or volume.
