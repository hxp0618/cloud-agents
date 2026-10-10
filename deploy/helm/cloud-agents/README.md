# Cloud Agents Helm deployment

By default this chart installs the no-Agent foundation: Identity, Control Plane,
Access Gateway, and independent HTTPS User/Admin Web services against a deployment-owned
PostgreSQL database. The legacy Coding Agent Worker is rendered only when
`worker.enabled=true`; only that compatibility mode requires the Worker TLS,
admission, Runtime environment and Provider credential Secrets. Build or load
the enabled images from the same platform release, create only the Secrets used
by that mode, then install with digest-pinned image values.

The Access Gateway mounts only the runtime database URL, its TLS/SSH identity,
and the configured Kubernetes target credential directory. User Web and Admin
Web receive separate TLS identities, separate Identity service proofs, and only
the Identity and Control Plane CA bundles. User Web proxies non-Admin `/v1`
routes and Admin Web proxies only `/v1/admin`. None of these Pods receives a
Docker socket or Provider credentials, and all disable ServiceAccount token mounting.

Create the database Secret with `runtime-url`, `migration-url`,
`tenant-bootstrap-url`, `identity-service-url`, and `identity-bootstrap-url`.
The last two authenticate distinct LOGIN roles: the long-running Identity
service role and the offline bootstrap role. Also create the Secrets named by
`identity.tls`, `identity.signing`, `identity.csrf`, `identity.providerFlow`, `identity.credentials`,
`adminWeb.tlsSecretName`, and `userWeb.tlsSecretName`. The credentials Secret
contains four distinct 43-character canonical base64url proofs: Admin Web, User
Web, Control Plane-to-Identity, and Identity-to-Control-Plane. The chart never
creates or prints these secret values.

The CSRF and provider-flow keys are independent 32-byte random files. For external
login, set `identity.providers.secretName` to an existing Secret and map
`identity.providers.clientSecrets` and `identity.providers.rootCAs` from logical
reference names to keys in that Secret. The init container copies only the mapped
files to Identity's private memory volume. Configure those logical references in
Admin Web; client-secret values never enter values files or the database. Leave
both maps empty for password-only operation. An omitted root CA reference uses
the system trust store. Restart Identity after changing provider material.
Platform administrators configure client IDs, issuer, reference names and
Admin/User callback URLs in Admin Web's Login Providers system settings. Identity
persists these application settings in its database and reads them for each login;
they are not chart values or product environment variables, and changes do not
require a restart. Each provider application must register the exact HTTPS
`/auth/provider/callback` URL displayed for its Admin/User configuration.

The Identity and Control Plane certificates must include the chart-generated
`<release>-cloud-agents-identity` and `<release>-cloud-agents-control-plane`
Service DNS names. The Admin/User certificates must match the distinct hostnames
in `identity.adminAudience` and `identity.userAudience`.

Keep `identityBootstrap.enabled=false` for upgrades and existing databases. For
a fresh install, pre-hash the initial password with
`cloud-agents-identity hash-password`, create the bootstrap Secret with the
setup proof and password hash, set a bounded signing-key interval, and enable
the one-time pre-install Job. The Job creates the first platform administrator
through the bootstrap-only database function and has no default password.
Enable `tenantBootstrap` in the same fresh-install values to create the initial
tenant binding for that stable user ID. Disable both bootstrap Jobs after the
successful install; existing users, bindings and signing lineage are never
rewritten.

When `worker.enabled=true`, the chart mounts a persistent snapshot archive at `/snapshots` in the Control Plane and sets `CLOUD_AGENTS_PLATFORM_SNAPSHOT_DIRECTORY`; use `runtime.snapshot.existingClaim` for an existing claim or let the chart create its single-writer `ReadWriteOnce` claim. The Kubernetes Target credential must permit the namespaced helper Pod lifecycle and `get/create` on `pods/exec` for portable Workspace snapshot export/import.

To register Kubernetes Targets from a kubeconfig in Admin Web, set
`deploymentTargets.credentialKeySecretName` to a Secret whose
`target-credential.key` holds exactly 32 random bytes. The Control Plane uses
it to seal the selected context's credential in PostgreSQL. Keep the key stable
and backed up; without it those Targets fail closed. It requires
`deploymentTargets.kubernetesCredentialSecretName` for the deployment
descriptors.

Private access-grant, target-credential, Identity, Web TLS and SSH-host material is copied by
non-root init containers into memory-backed volumes with mode `0400`; serving
containers do not mount the projected source Secrets. Keep the Services private
unless deployment-owned ingress is configured. A successful `helm lint` or Pod
rollout does not replace the real login, Target, Sandbox, backup/restore,
upgrade, and key/credential rotation acceptance requirements.

Run the packaged smoke with both adjacent release directories to verify an N-1
install, N upgrade, N-1 rollback, and final N re-upgrade against the same
PostgreSQL data and deployment-owned Secrets:

```sh
CLOUD_AGENTS_HELM_CONTEXT=orbstack \
  sh test/scripts/test-platform-helm.sh /path/to/release-n /path/to/release-n-1
```

The script accepts one release directory for the ordinary install smoke. The
two-release form is destructive only inside its exact-labelled temporary
namespace and local test image names; it does not validate external PostgreSQL,
OIDC ingress, S3, or cross-version schema changes when both releases carry the
same migration head.

The RemoteWorker CA Secret is deployment-owned. Rotate its trust root without
stranding customer nodes by creating a new Secret whose `ca.crt` contains the
new certificate followed by the old certificate and whose `ca.key` is the new
key, then point `remoteWorker.certificateAuthoritySecretName` at that immutable
Secret with `helm upgrade`. Confirm an existing node reconnects, stop its
supervisor, and run `run.sh --rotate-certificate-once --once` to issue a leaf
from the new root before restarting the supervisor. After all active nodes use
the new root, create another Secret containing only the new certificate and key
and switch the Helm value again. Retain the prior Secrets until rollback is no
longer required; never remove the old root while an active node depends on it.

Rotate the shared deployment service root in three immutable-Secret phases.
First point `tls.controlPlaneSecretName`, `tls.workerSecretName`,
`adminWeb.controlPlaneCASecretName`, and `userWeb.controlPlaneCASecretName` at copies of the current leaf identities whose
`ca.crt` contains the new root followed by the old root; update CLI and
RemoteWorker server trust bundles to the same overlap. Next issue new Control
Plane, Worker, Worker-client, and Access Gateway leaves from the new root and
switch all four `tls.*SecretName` values while retaining the overlap bundle.
After User/Admin proxying, Worker mTLS, Gateway TLS, and customer-node reconnects pass,
switch the Control Plane, Worker, User/Admin Web, CLI, and node trust bundles to the new
root only and verify old-root clients fail. The chart uses `Recreate`, so these
steps are recoverable root rotation, not a zero-downtime guarantee; retain each
prior Secret until its rollback window closes.
