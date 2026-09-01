#!/bin/sh

set -eu

: "${CLOUD_AGENTS_KUBECONFIG:?set the operator kubeconfig}"
: "${CLOUD_AGENTS_KUBERNETES_CONTEXT:?set the explicit kubeconfig context}"
: "${CLOUD_AGENTS_KUBERNETES_NAMESPACE:?set the target Worker namespace}"
: "${CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT:?set the target ServiceAccount name}"
: "${CLOUD_AGENTS_KUBERNETES_TOKEN_DURATION:?set the requested ServiceAccount token duration, for example 24h}"
: "${CLOUD_AGENTS_TARGET_CREDENTIAL_REF:?set the Control Plane Kubernetes credential reference}"
: "${CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR:?set the Control Plane Kubernetes credential directory}"
: "${CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY:?set the Worker image repository without a tag or digest}"
: "${CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF:?set the target Worker credential Secret name}"
: "${CLOUD_AGENTS_WORKER_CREDENTIAL_DIR:?set the source Worker credential directory}"
: "${CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF:?set the target Provider credential Secret name}"
: "${CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR:?set the source tenant Provider credential directory}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id used by the Provider credential files}"
: "${CLOUD_AGENTS_WORKER_SPIFFE_ID:?set the Worker SPIFFE ID}"
: "${CLOUD_AGENTS_WORKER_SERVER_NAME:?set the Worker TLS server name}"

kubectl=${KUBECTL-kubectl}
command -v "$kubectl" >/dev/null
command -v openssl >/dev/null

if [ ! -f "$CLOUD_AGENTS_KUBECONFIG" ] || [ -L "$CLOUD_AGENTS_KUBECONFIG" ]; then
  echo "kubeconfig must be a non-symlink regular file" >&2
  exit 1
fi
case "$CLOUD_AGENTS_KUBECONFIG" in
  /*) ;;
  *) echo "kubeconfig must be absolute" >&2; exit 1 ;;
esac
for directory in "$CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR" "$CLOUD_AGENTS_WORKER_CREDENTIAL_DIR" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR"; do
  if [ ! -d "$directory" ] || [ -L "$directory" ] || [ "$directory" = / ]; then
    echo "credential directories must be non-symlink directories other than root" >&2
    exit 1
  fi
  case "$directory" in
    /*) ;;
    *) echo "credential directories must be absolute" >&2; exit 1 ;;
  esac
done
output_dir=$(CDPATH= cd -- "$CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR" && pwd -P)
worker_dir=$(CDPATH= cd -- "$CLOUD_AGENTS_WORKER_CREDENTIAL_DIR" && pwd -P)
provider_dir=$(CDPATH= cd -- "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR" && pwd -P)
if [ "$output_dir" = "$worker_dir" ] || [ "$output_dir" = "$provider_dir" ] || [ "$worker_dir" = "$provider_dir" ]; then
  echo "output, Worker, and Provider credential directories must be distinct" >&2
  exit 1
fi

for identifier in "$CLOUD_AGENTS_TARGET_CREDENTIAL_REF" "$CLOUD_AGENTS_TENANT"; do
  case "$identifier" in
    *[!A-Za-z0-9._~-]*|''|[!A-Za-z0-9]*|*[!A-Za-z0-9]) echo "credential reference and tenant contain unsupported characters" >&2; exit 1 ;;
  esac
  if [ "${#identifier}" -gt 128 ]; then
    echo "credential reference and tenant must be at most 128 characters" >&2
    exit 1
  fi
done
for name in "$CLOUD_AGENTS_KUBERNETES_NAMESPACE" "$CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT" "$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF"; do
  case "$name" in
    *[!a-z0-9.-]*|''|[.-]*|*[.-]) echo "Kubernetes names must be lowercase DNS names" >&2; exit 1 ;;
  esac
done
if [ "${#CLOUD_AGENTS_KUBERNETES_NAMESPACE}" -gt 63 ] || [ "${#CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT}" -gt 40 ] ||
  [ "${#CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF}" -gt 253 ] || [ "${#CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF}" -gt 253 ]; then
  echo "Kubernetes name is too long" >&2
  exit 1
fi
if [ "$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF" = "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF" ]; then
  echo "Worker and Provider credential Secrets must be distinct" >&2
  exit 1
fi
if ! printf '%s\n' "$CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY" | grep -Eq '^[a-z0-9]+([.-][a-z0-9]+)*(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)+$'; then
  echo "Worker image repository must not contain a tag or digest" >&2
  exit 1
fi
case "$CLOUD_AGENTS_WORKER_SPIFFE_ID" in
  spiffe://*/*) ;;
  *) echo "Worker SPIFFE ID is invalid" >&2; exit 1 ;;
esac
case "${CLOUD_AGENTS_WORKER_SPIFFE_ID#spiffe://}" in
  ''|/*) echo "Worker SPIFFE ID is invalid" >&2; exit 1 ;;
esac
case "$CLOUD_AGENTS_WORKER_SPIFFE_ID" in
  *[!A-Za-z0-9._:/~-]*) echo "Worker SPIFFE ID contains unsupported characters" >&2; exit 1 ;;
esac
case "$CLOUD_AGENTS_WORKER_SERVER_NAME" in
  *[!A-Za-z0-9.-]*) echo "Worker server name contains unsupported characters" >&2; exit 1 ;;
esac
if [ "${#CLOUD_AGENTS_WORKER_SERVER_NAME}" -gt 253 ]; then
  echo "Worker server name is too long" >&2
  exit 1
fi

for file in server.crt server.key client-ca.crt admission-token; do
  if [ ! -s "$worker_dir/$file" ] || [ -L "$worker_dir/$file" ]; then
    echo "Worker credential directory is incomplete" >&2
    exit 1
  fi
done
set -- "$provider_dir/$CLOUD_AGENTS_TENANT".*.json
if [ ! -s "$1" ]; then
  echo "Provider credential directory has no tenant envelope" >&2
  exit 1
fi
for file in "$@"; do
  if [ ! -s "$file" ] || [ -L "$file" ]; then
    echo "Provider credential directory contains an invalid tenant envelope" >&2
    exit 1
  fi
done
for suffix in ca.crt token deployment.json; do
  if [ -e "$output_dir/$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.$suffix" ] || [ -L "$output_dir/$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.$suffix" ]; then
    echo "Kubernetes target credential output already exists; refusing to overwrite it" >&2
    exit 1
  fi
done

run_cluster() {
  "$kubectl" --kubeconfig "$CLOUD_AGENTS_KUBECONFIG" --context "$CLOUD_AGENTS_KUBERNETES_CONTEXT" "$@"
}
run_target() {
  run_cluster --namespace "$CLOUD_AGENTS_KUBERNETES_NAMESPACE" "$@"
}

target_endpoint=$(run_cluster config view --raw --minify --flatten -o 'jsonpath={.clusters[0].cluster.server}')
target_endpoint=${target_endpoint%/}
case "${target_endpoint#https://}" in
  "$target_endpoint"|''|*/*|*'?'*|*'#'*|*'@'*) echo "Kubernetes API endpoint is invalid" >&2; exit 1 ;;
esac

role_name="cloud-agents-target-$CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT"
run_cluster apply --server-side --field-manager=cloud-agents-target-prepare -f - >/dev/null <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $CLOUD_AGENTS_KUBERNETES_NAMESPACE
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT
  namespace: $CLOUD_AGENTS_KUBERNETES_NAMESPACE
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: $role_name
  namespace: $CLOUD_AGENTS_KUBERNETES_NAMESPACE
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF", "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF"]
    verbs: ["get"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "create", "patch", "delete"]
  - apiGroups: [""]
    resources: ["services", "persistentvolumeclaims"]
    verbs: ["get", "list", "create", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: $role_name
  namespace: $CLOUD_AGENTS_KUBERNETES_NAMESPACE
subjects:
  - kind: ServiceAccount
    name: $CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT
    namespace: $CLOUD_AGENTS_KUBERNETES_NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: $role_name
EOF

service_account="system:serviceaccount:$CLOUD_AGENTS_KUBERNETES_NAMESPACE:$CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT"
can_i() {
  if [ "$(run_target auth can-i --as="$service_account" "$@")" != yes ]; then
    echo "ServiceAccount lacks required Kubernetes permission: $*" >&2
    exit 1
  fi
}
can_i get /version
can_i get "secret/$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF"
can_i get "secret/$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF"
for resource in deployments.apps services persistentvolumeclaims; do
  for verb in get list create patch delete; do
    can_i "$verb" "$resource"
  done
done

for secret in "$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF"; do
  if [ -n "$(run_target get secret "$secret" --ignore-not-found -o name)" ]; then
    echo "Kubernetes credential Secret $secret already exists; refusing to overwrite it" >&2
    exit 1
  fi
done

temporary_dir=$(mktemp -d "$output_dir/.kubernetes-target-$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  rm -rf -- "$temporary_dir"
  exit "$status"
}
trap cleanup EXIT HUP INT TERM
umask 077

run_target create token "$CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT" --duration="$CLOUD_AGENTS_KUBERNETES_TOKEN_DURATION" >"$temporary_dir/token"
ca_data=$(run_cluster config view --raw --minify --flatten -o 'jsonpath={.clusters[0].cluster.certificate-authority-data}')
printf '%s' "$ca_data" | openssl base64 -d -A >"$temporary_dir/ca.crt"
if [ ! -s "$temporary_dir/token" ] || ! openssl x509 -in "$temporary_dir/ca.crt" -noout >/dev/null 2>&1; then
  echo "Kubernetes target token or CA is invalid" >&2
  exit 1
fi
printf '{"namespace":"%s","workerImageRepository":"%s","workerCredentialSecretRef":"%s","workerSpiffeId":"%s","workerServerName":"%s"}\n' \
  "$CLOUD_AGENTS_KUBERNETES_NAMESPACE" "$CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY" "$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF" \
  "$CLOUD_AGENTS_WORKER_SPIFFE_ID" "$CLOUD_AGENTS_WORKER_SERVER_NAME" >"$temporary_dir/deployment.json"
chmod 0400 "$temporary_dir/ca.crt" "$temporary_dir/token" "$temporary_dir/deployment.json"

run_target create secret generic "$CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF" \
  --from-file="server.crt=$worker_dir/server.crt" --from-file="server.key=$worker_dir/server.key" \
  --from-file="client-ca.crt=$worker_dir/client-ca.crt" --from-file="admission-token=$worker_dir/admission-token" >/dev/null
set -- create secret generic "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF"
for file in "$provider_dir/$CLOUD_AGENTS_TENANT".*.json; do
  set -- "$@" "--from-file=${file##*/}=$file"
done
run_target "$@" >/dev/null

ln "$temporary_dir/ca.crt" "$output_dir/$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.ca.crt"
ln "$temporary_dir/token" "$output_dir/$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.token"
ln "$temporary_dir/deployment.json" "$output_dir/$CLOUD_AGENTS_TARGET_CREDENTIAL_REF.deployment.json"
printf 'Kubernetes target prepared: endpoint=%s credentialRef=%s\n' "$target_endpoint" "$CLOUD_AGENTS_TARGET_CREDENTIAL_REF"
