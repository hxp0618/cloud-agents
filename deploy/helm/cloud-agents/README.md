# Cloud Agents Helm deployment

This chart installs the Control Plane, Worker, Access Gateway, and independent Admin Web against a deployment-owned PostgreSQL database. Build or load every image from the same platform release, create the Secrets named in `values.yaml`, then install with digest-pinned image values.

The Access Gateway mounts only the runtime database URL, its TLS/SSH identity, and the configured Kubernetes target credential directory. The Admin Web mounts only the Control Plane CA and proxies `/v1/admin` to the internal Control Plane Service. Neither Pod receives a Docker socket or Provider credentials, and both disable ServiceAccount token mounting.

The access-grant and SSH host private keys are copied by non-root init containers into memory-backed volumes with mode `0400`; projected Secret files are not exposed to the serving containers. Keep the Admin Web and Gateway Services private unless a deployment-owned TLS/OIDC ingress is configured. A successful `helm lint` or Pod rollout does not replace the real Target, Sandbox, backup/restore, upgrade, and identity-rotation acceptance requirements.
