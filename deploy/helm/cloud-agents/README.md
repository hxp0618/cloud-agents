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
