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
grep -Fq "mountPath: /tmp" "$rendered"
grep -Fq "emptyDir: {}" "$rendered"
grep -A1 -F -- "- --runtime-max-sessions" "$rendered" | grep -Fq -- '- "4"'

helm template cloud-agents "$chart" \
  --set-string images.controlPlane.digest="$digest" \
  --set-string images.worker.digest="$digest" \
  --set-string images.migrate.digest="$digest" >"$rendered"

for image in control-plane worker migrate; do
  grep -Fq "image: \"cloud-agents/$image@$digest\"" "$rendered"
done

if helm template cloud-agents "$chart" --set-string images.worker.digest=sha256:invalid >/dev/null 2>&1; then
  echo "invalid OCI image digest passed Helm values validation" >&2
  exit 1
fi
if helm template cloud-agents "$chart" --set runtime.maxSessions=0 >/dev/null 2>&1; then
  echo "invalid Runtime max sessions passed Helm values validation" >&2
  exit 1
fi
