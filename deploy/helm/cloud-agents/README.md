# Cloud Agents Helm deployment

This chart installs the Control Plane, Worker, Access Gateway, and independent Admin Web against a deployment-owned PostgreSQL database. Build or load every image from the same platform release, create the Secrets named in `values.yaml`, then install with digest-pinned image values.

The Access Gateway mounts only the runtime database URL, its TLS/SSH identity, and the configured Kubernetes target credential directory. The Admin Web mounts only the Control Plane CA and proxies `/v1/admin` to the internal Control Plane Service. Neither Pod receives a Docker socket or Provider credentials, and both disable ServiceAccount token mounting.

The access-grant and SSH host private keys are copied by non-root init containers into memory-backed volumes with mode `0400`; projected Secret files are not exposed to the serving containers. Keep the Admin Web and Gateway Services private unless a deployment-owned TLS/OIDC ingress is configured. A successful `helm lint` or Pod rollout does not replace the real Target, Sandbox, backup/restore, upgrade, and identity-rotation acceptance requirements.

Run the packaged smoke with both adjacent release directories to verify an N-1
install, N upgrade, N-1 rollback, and final N re-upgrade against the same
PostgreSQL data and deployment-owned Secrets:

```sh
CLOUD_AGENTS_HELM_CONTEXT=orbstack \
  sh scripts/test-platform-helm.sh /path/to/release-n /path/to/release-n-1
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
First point `tls.controlPlaneSecretName`, `tls.workerSecretName`, and
`adminWeb.controlPlaneCASecretName` at copies of the current leaf identities whose
`ca.crt` contains the new root followed by the old root; update CLI and
RemoteWorker server trust bundles to the same overlap. Next issue new Control
Plane, Worker, Worker-client, and Access Gateway leaves from the new root and
switch all four `tls.*SecretName` values while retaining the overlap bundle.
After Admin proxying, Worker mTLS, Gateway TLS, and customer-node reconnects pass,
switch the Control Plane, Worker, Admin, CLI, and node trust bundles to the new
root only and verify old-root clients fail. The chart uses `Recreate`, so these
steps are recoverable root rotation, not a zero-downtime guarantee; retain each
prior Secret until its rollback window closes.
