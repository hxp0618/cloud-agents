#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
chart=$repository_root/deploy/helm/cloud-agents
digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
rendered=$(mktemp "${TMPDIR:-/tmp}/cloud-agents-helm.XXXXXX")
trap 'rm -f -- "$rendered"' EXIT HUP INT TERM

helm lint "$chart"
helm template cloud-agents "$chart" >"$rendered"
for image in control-plane worker migrate; do
  grep -Fq "image: \"cloud-agents/$image:0.2.0\"" "$rendered"
done
grep -Fq "fsGroup: 1000" "$rendered"
grep -Fq "mountPath: /workspace" "$rendered"
grep -A1 -F -- "- --runtime-directory" "$rendered" | grep -Fq -- '- /workspace'
grep -Fq "mountPath: /tmp" "$rendered"
grep -Fq "emptyDir: {}" "$rendered"
grep -A1 -F -- "- --runtime-max-sessions" "$rendered" | grep -Fq -- '- "4"'
grep -A1 -F -- "- --max-concurrent-requests" "$rendered" | grep -Fq -- '- "128"'

helm template cloud-agents "$chart" \
  --set-string images.controlPlane.digest="$digest" \
  --set-string images.worker.digest="$digest" \
  --set-string images.migrate.digest="$digest" >"$rendered"

for image in control-plane worker migrate; do
  grep -Fq "image: \"cloud-agents/$image@$digest\"" "$rendered"
done

helm template cloud-agents "$chart" \
  --set-string remoteWorker.certificateAuthoritySecretName=cloud-agents-remote-worker-ca \
  --set-string remoteWorker.trustDomain=remote-worker.example >"$rendered"
grep -Fq "name: CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_CERT" "$rendered"
grep -Fq "value: /run/cloud-agents/remote-worker-ca/ca.key" "$rendered"
grep -Fq "value: \"remote-worker.example\"" "$rendered"
grep -Fq "secretName: cloud-agents-remote-worker-ca" "$rendered"

if helm template cloud-agents "$chart" --set-string images.worker.digest=sha256:invalid >/dev/null 2>&1; then
  echo "invalid OCI image digest passed Helm values validation" >&2
  exit 1
fi
if helm template cloud-agents "$chart" --set runtime.maxSessions=0 >/dev/null 2>&1; then
  echo "invalid Runtime max sessions passed Helm values validation" >&2
  exit 1
fi
if helm template cloud-agents "$chart" --set controlPlane.maxConcurrentRequests=0 >/dev/null 2>&1; then
  echo "invalid Control Plane max concurrent requests passed Helm values validation" >&2
  exit 1
fi
if helm template cloud-agents "$chart" --set-string remoteWorker.certificateAuthoritySecretName=cloud-agents-remote-worker-ca >/dev/null 2>&1; then
  echo "partial RemoteWorker certificate authority passed Helm values validation" >&2
  exit 1
fi
