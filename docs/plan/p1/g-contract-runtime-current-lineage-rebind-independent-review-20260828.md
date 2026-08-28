# G-CONTRACT current-runtime lineage rebind independent review — 2026-08-28

Date: 2026-08-28 (Asia/Shanghai)  
Review type: independent, fixed-object, read-only closed-pair review  
Purpose: provide a versioned successor binding for the current control-plane
module bytes consumed by the D-053 generated closure profile.

## Verdict

`APPROVE` — P0=0 / P1=0 / P2=0.

This review approves only the bounded runtime-criterion lineage rebind. It
does not rewrite the historical v3 closure source, schema, profile, receipt,
or review objects; those remain recoverable predecessors. It does not authorize
native or production Runner use, PostgreSQL or migration writes, HTTP/OIDC/JWKS,
P2/provider effects, deployment, publication, release, or any Gate transition.
`notGateClosure=true` and `gateStatus=ALL_GATES_OPEN` remain mandatory.

## Fixed candidate and closed pair

The candidate is a single-parent commit already independently reviewed for the
current local execution/module boundary. The review child for that candidate is
the existing D-055 review; this record adds an independent check that the
candidate is suitable as the current runtime-module predecessor for the
superseding closure profile.

| item | identity |
| --- | --- |
| candidate commit | `b79d01028c652d004e67a00fdcbdf204e04dc946` |
| candidate tree | `289c7c2ff7ab39b0af1ea0bac84a902d461de8dc` |
| candidate parent | `4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97` |
| candidate-parent binary diff SHA-256 | `sha256:e967207e24167e8461fbffbbc98df41103e06eacc508f1bc9baca289433b639c` |
| candidate module review child | `5abcdfc519c9053aa8e1437fa15e6e498e606e28` |
| module review tree | `78be2254bdf2145c241560382addedb34b40ad09` |
| module review parent | `b79d01028c652d004e67a00fdcbdf204e04dc946` |
| module review path | `docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md` |
| module review raw SHA-256 / bytes | `sha256:ad7ba17696cc405b5507212bc9a228360d4418bfd46e793eb51cf6fc71f6bd3f` / `5737` |

The candidate and review are both commit objects, the review is a direct
single-parent child, and neither object contains this record. No merge parent,
working-tree state, or caller-selected ref is accepted as lineage evidence.

## Current module bytes

The candidate carries the exact bytes now present on the approved P0 line:

| path | mode | Git blob | raw SHA-256 | bytes |
| --- | --- | --- | --- | ---: |
| `services/control-plane/go.mod` | `100644` | `8e7f87cadf8b6bb283230fcc1b9a1b2466e6ca73` | `sha256:d27871e7d4d8788d455ac2a5b9d512b0b6628903fad05213a9e227c0f0883d3d` | 672 |
| `services/control-plane/go.sum` | `100644` | `e516097c321550eba034aa50d1039a1bd1e81ac0` | `sha256:4b870f580591894010f0762c8d04b83cba95a5c09eabc4ffc2631e41290abfbc` | 3634 |

The same mode, size, Git blob, and raw digest were read from the current P0
checkout. A mismatch in any one field is a hard failure; no size-only or
recomputed-self-consistent substitute is accepted.

## Runtime criterion carry-forward

The runtime server/tenant-authority files reviewed by the historical runtime
pair remain byte-identical at the fixed candidate:

| path | Git blob |
| --- | --- |
| `services/control-plane/internal/server/managed_agent_create_project.go` | `52545f173291f1e2655eb914746d783552a57f06` |
| `services/control-plane/internal/server/managed_agent_create_project_test.go` | `abc1b07a6f02a3ea40a93a11bfbf34a8ed176a46` |
| `services/control-plane/internal/authn/runtime_server_external_test.go` | `2a964d332a01bb0785075dacbe1e8cd28eb41852` |
| `services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh` | `f2891ad9eb7de1f7233a9eb03f4e50801bbd7864` |

The historical runtime review remains the semantic evidence for those files;
this closed-pair review adds the current module identity and confirms that the
rebind does not alter their bytes. The D-055 review independently found no
P0/P1/P2 issue in the additional localdev worker changes and records the same
module bytes. The two records are references, not mutable inputs.

## Checks and boundary

Read-only checks performed for this record:

- verified candidate/review object type, exact tree, direct parents, and the
  candidate-parent binary diff digest with fixed Git environment;
- compared both module files against the current P0 checkout by mode, size,
  Git blob, and raw SHA-256;
- compared the four runtime server/matrix blobs against the historical review;
- verified the existing D-055 review verdict is `APPROVE` with P0/P1/P2 all zero;
- confirmed no HTTP listener/client, PostgreSQL write, provider/P2 call,
  deployment, publication, release, SSH, hardware, or Gate actuator is part of
  this rebind.

This record is a closed-pair input for a later generated closure successor. It
does not itself claim native replay success, assemble a generation lock, or
permit any external side effect. Any later source/schema/profile or projection
must bind this exact pair and be independently reviewed as a separate
append-only candidate.

