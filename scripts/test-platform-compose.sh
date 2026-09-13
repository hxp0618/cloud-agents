#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-compose.sh PLATFORM_RELEASE_DIRECTORY [REAL_PROVIDER_CREDENTIALS_DIRECTORY]" >&2
  exit 2
fi
real_provider_credentials_directory=
real_provider_test=0
if [ "$#" -eq 2 ]; then
  case "$2" in
    /*) ;;
    *) echo "real Provider credentials directory must be absolute" >&2; exit 2 ;;
  esac
  if [ ! -d "$2" ] || [ -L "$2" ]; then
    echo "real Provider credentials directory must be a non-symlink directory" >&2
    exit 2
  fi
  real_provider_credentials_directory=$(CDPATH= cd -- "$2" && pwd -P)
  real_provider_test=1
  for provider in codex claudeAgent pi deepseek-harness; do
    credential_file="$real_provider_credentials_directory/tenant-compose-smoke.$provider.json"
    if [ ! -f "$credential_file" ] || [ -L "$credential_file" ]; then
      echo "real Provider credentials directory is missing a non-symlink tenant-compose-smoke.$provider.json" >&2
      exit 2
    fi
  done
fi
cross_node_recovery=${CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY:-0}
case "$cross_node_recovery" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY must be 0 or 1" >&2; exit 2 ;;
esac
cross_node_provider=${CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER:-codex}
case "$cross_node_provider" in
  codex | claudeAgent | pi | deepseek-harness) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER contains unsupported Provider $cross_node_provider" >&2; exit 2 ;;
esac
cross_node_environment=${CLOUD_AGENTS_COMPOSE_CROSS_NODE_ENVIRONMENT:-docker}
case "$cross_node_environment" in
  docker | kubernetes | remote-worker) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CROSS_NODE_ENVIRONMENT must be docker, remote-worker or kubernetes" >&2; exit 2 ;;
esac
docker_cross_node_recovery_completed=0
remote_runtime=${CLOUD_AGENTS_COMPOSE_REMOTE_RUNTIME:-0}
case "$remote_runtime" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_REMOTE_RUNTIME must be 0 or 1" >&2; exit 2 ;;
esac
kubernetes_runtime=${CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME:-0}
case "$kubernetes_runtime" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME must be 0 or 1" >&2; exit 2 ;;
esac
kubernetes_context=${CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT-}
kubernetes_kubeconfig=${CLOUD_AGENTS_COMPOSE_KUBECONFIG-}
if [ "$kubernetes_runtime" -eq 1 ]; then
  if [ -z "$real_provider_credentials_directory" ] || [ -z "$kubernetes_context" ] ||
    [ -z "$kubernetes_kubeconfig" ] || [ ! -f "$kubernetes_kubeconfig" ] || [ -L "$kubernetes_kubeconfig" ]; then
    echo "Kubernetes Runtime smoke requires real Provider credentials, an explicit context, and a regular kubeconfig" >&2
    exit 2
  fi
  case "$kubernetes_kubeconfig" in
    /*) ;;
    *) echo "CLOUD_AGENTS_COMPOSE_KUBECONFIG must be absolute" >&2; exit 2 ;;
  esac
fi
kind_cluster=${CLOUD_AGENTS_COMPOSE_KIND_CLUSTER-}
kubernetes_source_node=${CLOUD_AGENTS_COMPOSE_KUBERNETES_SOURCE_NODE-}
kubernetes_destination_node=${CLOUD_AGENTS_COMPOSE_KUBERNETES_DESTINATION_NODE-}
kind_control_plane=
kind_network=
if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
  if [ "$kubernetes_runtime" -ne 1 ] || [ -z "$kind_cluster" ] || [ -z "$kubernetes_source_node" ] ||
    [ -z "$kubernetes_destination_node" ] || [ "$kubernetes_source_node" = "$kubernetes_destination_node" ]; then
    echo "Kubernetes cross-node recovery requires Kubernetes Runtime, a kind cluster, and distinct source/destination nodes" >&2
    exit 2
  fi
  command -v kind >/dev/null 2>&1 || { echo "Kubernetes cross-node recovery requires kind" >&2; exit 2; }
  kind_control_plane=$(kind get nodes --name "$kind_cluster" | sed -n '/-control-plane$/p')
  case "$kind_control_plane" in
    '' | *' '*) echo "Kubernetes cross-node recovery requires exactly one kind control-plane node" >&2; exit 2 ;;
  esac
  kind_network=$(docker inspect "$kind_control_plane" --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}')
  case "$kind_network" in
    '' | *' '*) echo "Kubernetes cross-node recovery requires exactly one kind Docker network" >&2; exit 2 ;;
  esac
fi
real_provider_kinds=${CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS-"codex claudeAgent pi deepseek-harness"}
[ -n "$real_provider_kinds" ] || { echo "CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS must select at least one Provider" >&2; exit 2; }
for provider in $real_provider_kinds; do
  case "$provider" in
    codex | claudeAgent | pi | deepseek-harness) ;;
    *) echo "CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS contains unsupported Provider $provider" >&2; exit 2 ;;
  esac
done
sdk_live=${CLOUD_AGENTS_COMPOSE_SDK_LIVE:-0}
case "$sdk_live" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_SDK_LIVE must be 0 or 1" >&2; exit 2 ;;
esac
if [ "$sdk_live" -eq 1 ] && [ "$real_provider_test" -ne 1 ]; then
  echo "live SDK consumer smoke requires real Provider credentials" >&2
  exit 2
fi
if [ "$remote_runtime" -eq 1 ] && { [ "$cross_node_recovery" -ne 1 ] || [ -z "$real_provider_credentials_directory" ]; }; then
  echo "Remote Runtime smoke requires cross-node recovery and real Provider credentials" >&2
  exit 2
fi
for command in cmp curl docker node openssl ssh ssh-keygen; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "platform Compose smoke requires $command" >&2
    exit 2
  fi
done
if [ "$sdk_live" -eq 1 ]; then
  for command in bun go npm tar zip; do
    command -v "$command" >/dev/null 2>&1 || { echo "live SDK consumer smoke requires $command" >&2; exit 2; }
  done
fi
if [ "$kubernetes_runtime" -eq 1 ]; then
  command -v kubectl >/dev/null 2>&1 || { echo "Kubernetes Runtime smoke requires kubectl" >&2; exit 2; }
  kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" cluster-info >/dev/null
fi
docker_host=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
case "$docker_host" in
  unix:///*) docker_socket=${docker_host#unix://} ;;
  *) echo "platform Compose Docker target smoke requires a Unix Docker context" >&2; exit 2 ;;
esac
docker_gateway=$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
script_directory=$(CDPATH= cd -- "$(dirname "$0")" && pwd -P)
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) cli_target=darwin-arm64; image_platform=linux/arm64 ;;
  Darwin/x86_64) cli_target=darwin-amd64; image_platform=linux/amd64 ;;
  Linux/aarch64 | Linux/arm64) cli_target=linux-arm64; image_platform=linux/arm64 ;;
  Linux/x86_64 | Linux/amd64) cli_target=linux-amd64; image_platform=linux/amd64 ;;
  *) echo "platform Compose smoke requires Darwin or Linux on amd64 or arm64" >&2; exit 2 ;;
esac
cli="$candidate_directory/cloud-agentsctl-$cli_target"
if [ ! -x "$cli" ]; then
  echo "platform release is missing executable cloud-agentsctl-$cli_target" >&2
  exit 1
fi
set -- "$candidate_directory"/cloud-agents-deployment-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one deployment package" >&2
  exit 1
fi

smoke_directory=$(mktemp -d "$candidate_directory/.compose-smoke.XXXXXX")
project="cloud-agents-compose-smoke-$$"
environment_file="$smoke_directory/compose.env"
base_environment_file="$smoke_directory/compose.no-agent.env"
compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.yml"
managed_agent_compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.managed-agent.yml"
remote_worker_compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.remote-worker.yml"
compose_override_file="$smoke_directory/compose-target-override.yml"
docker_proxy_pid=
opensandbox_proxy_pid=
kubernetes_opensandbox_proxy_pid=
kubernetes_destination_opensandbox_proxy_pid=
destination_docker_proxy_pid=
destination_opensandbox_proxy_pid=
kubernetes_api_pid=
registry_container="${project}-registry"
opensandbox_container="${project}-opensandbox"
destination_node_container="${project}-restore-node"
remote_worker_container="${project}-remote-worker"
remote_worker_destination_container="${project}-remote-worker-restore"
kubernetes_opensandbox_container="${project}-kubernetes-opensandbox"
kubernetes_destination_opensandbox_container="${project}-kubernetes-opensandbox-restore"
opensandbox_server_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb"
kubernetes_controller_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/controller:v0.2.0"
kubernetes_execd_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd"
kubernetes_egress_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a"
kubernetes_runtime_target_id=compose-kubernetes-runtime-target
kubernetes_runtime_credential_ref=compose-kubernetes-runtime
kubernetes_runtime_namespace="${project}-k8s"
kubernetes_destination_target_id=compose-kubernetes-runtime-target-restore
kubernetes_destination_credential_ref=compose-kubernetes-runtime-restore
kubernetes_destination_namespace="${project}-k8s-restore"
kubernetes_active_namespace=$kubernetes_runtime_namespace
kubernetes_operator_namespace="${project}-k8s-ops"
kubernetes_version_role="${project}-k8s-version"
kubernetes_version_binding="${project}-k8s-version"
kubernetes_manager_binding="${project}-k8s-manager"
kubernetes_destination_version_binding="${project}-k8s-version-restore"
kubernetes_destination_manager_binding="${project}-k8s-manager-restore"
foundation_workspace_id=compose-gateway-workspace
foundation_agent_workspace_id=compose-agent-workspace
foundation_agent_sandbox_id=compose-agent-sandbox
foundation_agent_runtime_id=
kubernetes_agent_workspace_id=compose-kubernetes-agent-workspace
kubernetes_agent_sandbox_id=compose-kubernetes-agent-sandbox
kubernetes_agent_generation=
kubernetes_agent_resource_version=
kubernetes_agent_environment_profile_id=compose-kubernetes-agent-environment-profile
worker_repository=
worker_release_digest=
worker_upgrade_release_digest=
target_worker_credentials_volume="${project}-target-worker-credentials"
target_provider_credentials_volume="${project}-target-provider-credentials"
retry_provider_credentials_volume="${project}-retry-provider-credentials"
project_id=
profile_environment_id=
compose() {
  if [ "$remote_runtime" -eq 1 ]; then
    docker compose --env-file "$environment_file" -f "$compose_file" -f "$managed_agent_compose_file" -f "$remote_worker_compose_file" -f "$compose_override_file" "$@"
  else
    docker compose --env-file "$environment_file" -f "$compose_file" -f "$managed_agent_compose_file" -f "$compose_override_file" "$@"
  fi
}
compose_base() {
  docker compose --env-file "$base_environment_file" -f "$compose_file" -f "$compose_override_file" "$@"
}
kubernetes_ctl() {
  kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" "$@"
}
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  docker rm -f "$kubernetes_opensandbox_container" >/dev/null 2>&1 || true
  docker rm -f "$kubernetes_destination_opensandbox_container" >/dev/null 2>&1 || true
  if [ "$kubernetes_runtime" -eq 1 ]; then
    for namespace in "$kubernetes_runtime_namespace" "$kubernetes_destination_namespace" "$kubernetes_operator_namespace"; do
      owner=$(kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" get namespace "$namespace" -o json 2>/dev/null |
        node -e 'const fs=require("node:fs");const input=fs.readFileSync(0,"utf8");if(input)process.stdout.write(JSON.parse(input).metadata?.labels?.["cloud-agents.dev/test-run"]??"")' || true)
      if [ "$owner" = "$project" ]; then
        kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" delete namespace "$namespace" --wait=true --timeout=180s >/dev/null 2>&1 || status=1
      elif [ -n "$owner" ]; then
        echo "refusing to delete Kubernetes namespace without the exact test ownership label: $namespace" >&2
        status=1
      fi
    done
    for resource in "clusterrolebinding/$kubernetes_version_binding" "clusterrolebinding/$kubernetes_manager_binding" \
      "clusterrolebinding/$kubernetes_destination_version_binding" "clusterrolebinding/$kubernetes_destination_manager_binding" \
      "clusterrole/$kubernetes_version_role"; do
      owner=$(kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" get "$resource" -o json 2>/dev/null |
        node -e 'const fs=require("node:fs");const input=fs.readFileSync(0,"utf8");if(input)process.stdout.write(JSON.parse(input).metadata?.labels?.["cloud-agents.dev/test-run"]??"")' || true)
      if [ "$owner" = "$project" ]; then
        kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" delete "$resource" --wait=true --timeout=60s >/dev/null 2>&1 || status=1
      elif [ -n "$owner" ]; then
        echo "refusing to delete Kubernetes resource without the exact test ownership label: $resource" >&2
        status=1
      fi
    done
    for container in $(docker ps -aq --filter "label=cloud-agents.dev/test-run=$project"); do
      docker rm -f "$container" >/dev/null 2>&1 || status=1
    done
  fi
  if [ "$status" -ne 0 ] && docker container inspect "$remote_worker_container" >/dev/null 2>&1; then
    docker logs "$remote_worker_container" >&2 || true
  fi
  if [ -n "$project_id" ]; then
    for container in $(docker ps -aq \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id"); do
      if [ "$status" -ne 0 ]; then
        docker logs "$container" >&2 || true
      fi
      docker rm -f "$container" >/dev/null 2>&1 || true
    done
  fi
  if [ -f "$environment_file" ]; then
    if [ "$status" -ne 0 ]; then
      compose logs --no-color --tail=200 user-web admin-web access-gateway access-gateway-ssh-key access-grant-key control-plane worker migrate postgres >&2 || true
    fi
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  if [ "$status" -ne 0 ]; then
    docker logs "$opensandbox_container" >&2 || true
  fi
  docker rm -f "$opensandbox_container" >/dev/null 2>&1 || true
  if [ -f "$smoke_directory/opensandbox-egress-baseline" ]; then
    for container in $(docker ps -aq --filter label=opensandbox.io/egress-sidecar-for); do
      if ! grep -Fqx "$container" "$smoke_directory/opensandbox-egress-baseline"; then
        if [ "$status" -ne 0 ]; then
          docker logs "$container" >&2 || true
        fi
        docker rm -f "$container" >/dev/null 2>&1 || true
      fi
    done
  fi
  if [ -f "$smoke_directory/opensandbox-runtime-baseline" ]; then
    for container in $(docker ps -aq --filter label=opensandbox.io/id); do
      if ! grep -Fqx "$container" "$smoke_directory/opensandbox-runtime-baseline"; then
        if [ "$status" -ne 0 ]; then
          docker logs "$container" >&2 || true
        fi
        docker rm -f "$container" >/dev/null 2>&1 || true
      fi
    done
  fi
  if [ -n "$project_id" ]; then
    for volume in $(docker volume ls -q \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id"); do
      docker volume rm "$volume" >/dev/null 2>&1 || true
    done
  fi
  if [ -f "$smoke_directory/opensandbox-volume-baseline" ]; then
    for volume in $(docker volume ls -q --filter label=opensandbox.io/volume-managed-by=server); do
      if ! grep -Fqx "$volume" "$smoke_directory/opensandbox-volume-baseline"; then
        docker volume rm "$volume" >/dev/null 2>&1 || true
      fi
    done
  fi
  docker rm -f "$remote_worker_container" >/dev/null 2>&1 || true
  docker rm -f "$remote_worker_destination_container" >/dev/null 2>&1 || true
  docker rm -f -v "$destination_node_container" >/dev/null 2>&1 || true
  if docker network inspect "${project}_default" >/dev/null 2>&1 &&
    ! docker network rm "${project}_default" >/dev/null 2>&1; then
    echo "Compose smoke network cleanup failed: ${project}_default" >&2
    status=1
  fi
  docker rm -f "$registry_container" >/dev/null 2>&1 || true
  docker rm -f "${project}-worker-upgrade-seed" >/dev/null 2>&1 || true
  docker volume rm "$target_worker_credentials_volume" "$target_provider_credentials_volume" \
    "$retry_provider_credentials_volume" >/dev/null 2>&1 || true
  if [ -n "$worker_repository" ]; then
    docker image rm "$worker_repository:smoke" "$worker_repository:upgrade" >/dev/null 2>&1 || true
  fi
  if [ -n "$worker_repository" ] && [ -n "$worker_release_digest" ]; then
    docker image rm "$worker_repository@$worker_release_digest" >/dev/null 2>&1 || true
  fi
  if [ -n "$worker_repository" ] && [ -n "$worker_upgrade_release_digest" ]; then
    docker image rm "$worker_repository@$worker_upgrade_release_digest" >/dev/null 2>&1 || true
  fi
  if [ -n "$docker_proxy_pid" ]; then
    kill "$docker_proxy_pid" >/dev/null 2>&1 || true
    wait "$docker_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$opensandbox_proxy_pid" ]; then
    kill "$opensandbox_proxy_pid" >/dev/null 2>&1 || true
    wait "$opensandbox_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$kubernetes_opensandbox_proxy_pid" ]; then
    kill "$kubernetes_opensandbox_proxy_pid" >/dev/null 2>&1 || true
    wait "$kubernetes_opensandbox_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$kubernetes_destination_opensandbox_proxy_pid" ]; then
    kill "$kubernetes_destination_opensandbox_proxy_pid" >/dev/null 2>&1 || true
    wait "$kubernetes_destination_opensandbox_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$destination_docker_proxy_pid" ]; then
    kill "$destination_docker_proxy_pid" >/dev/null 2>&1 || true
    wait "$destination_docker_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$destination_opensandbox_proxy_pid" ]; then
    kill "$destination_opensandbox_proxy_pid" >/dev/null 2>&1 || true
    wait "$destination_opensandbox_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$kubernetes_api_pid" ]; then
    kill "$kubernetes_api_pid" >/dev/null 2>&1 || true
    wait "$kubernetes_api_pid" 2>/dev/null || true
  fi
  docker image rm "${project}-user-web" "${project}-admin-web" "${project}-access-gateway" "${project}-control-plane" "${project}-worker" "${project}-migrate" >/dev/null 2>&1 || true
  docker image rm "${project}-opensandbox-server:fixed" >/dev/null 2>&1 || true
  rm -rf -- "$smoke_directory"
  exit "$status"
}
trap cleanup 0 HUP INT TERM

mkdir -p "$smoke_directory/deployment" "$smoke_directory/access-gateway-tls" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" "$smoke_directory/workspace" "$smoke_directory/snapshots" \
  "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/prepared-kubernetes-target-credentials" \
  "$smoke_directory/ssh-target-credentials" \
  "$smoke_directory/remote-worker-ca" "$smoke_directory/remote-worker-node" "$smoke_directory/remote-worker-node-restore" \
  "$smoke_directory/fake-kubectl-state" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
docker ps -aq --filter label=opensandbox.io/id | sort >"$smoke_directory/opensandbox-runtime-baseline"
docker ps -aq --filter label=opensandbox.io/egress-sidecar-for | sort >"$smoke_directory/opensandbox-egress-baseline"
docker volume ls -q --filter label=opensandbox.io/volume-managed-by=server | sort >"$smoke_directory/opensandbox-volume-baseline"
chmod 0755 "$smoke_directory" "$smoke_directory/access-gateway-tls" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" \
  "$smoke_directory/docker-target-credentials" "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/prepared-kubernetes-target-credentials" \
  "$smoke_directory/ssh-target-credentials" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
chmod 0700 "$smoke_directory/fake-kubectl-state"
chmod 0700 "$smoke_directory/remote-worker-ca" "$smoke_directory/remote-worker-node"
chmod 0700 "$smoke_directory/remote-worker-node-restore"
chmod 0777 "$smoke_directory/workspace" "$smoke_directory/snapshots"
tar -xf "$1" -C "$smoke_directory/deployment"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-compose-remote-worker-ca \
  -keyout "$smoke_directory/remote-worker-ca/ca.key" -out "$smoke_directory/remote-worker-ca/ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
chmod 0400 "$smoke_directory/remote-worker-ca/ca.key"
chmod 0444 "$smoke_directory/remote-worker-ca/ca.crt"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-compose-smoke-ca \
  -keyout "$smoke_directory/ca.key" -out "$smoke_directory/ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=worker \
  -keyout "$smoke_directory/worker-tls/server.key" -out "$smoke_directory/worker.csr" \
  -addext subjectAltName=DNS:worker,URI:spiffe://cloud-agents.compose/worker \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/worker.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/worker-tls/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=host.docker.internal \
  -keyout "$smoke_directory/target-worker-credentials/server.key" \
  -out "$smoke_directory/target-worker.csr" \
  -addext subjectAltName=DNS:host.docker.internal,URI:spiffe://cloud-agents.compose/worker-target \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/target-worker.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/target-worker-credentials/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane-worker-client \
  -keyout "$smoke_directory/control-plane-tls/worker-client.key" \
  -out "$smoke_directory/control-plane-client.csr" \
  -addext subjectAltName=URI:spiffe://cloud-agents.compose/control-plane \
  -addext extendedKeyUsage=clientAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/control-plane-client.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/control-plane-tls/worker-client.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane \
  -keyout "$smoke_directory/control-plane-tls/server.key" \
  -out "$smoke_directory/control-plane-server.csr" \
  -addext subjectAltName=DNS:control-plane,DNS:host.docker.internal,IP:127.0.0.1 \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/control-plane-server.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/control-plane-tls/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=access-gateway \
  -keyout "$smoke_directory/access-gateway-tls/server.key" \
  -out "$smoke_directory/access-gateway-server.csr" \
  -addext subjectAltName=DNS:access-gateway,IP:127.0.0.1 \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/access-gateway-server.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/access-gateway-tls/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=host.docker.internal \
  -keyout "$smoke_directory/opensandbox-proxy.key" \
  -out "$smoke_directory/opensandbox-proxy.csr" \
  -addext subjectAltName=DNS:host.docker.internal \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/opensandbox-proxy.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/opensandbox-proxy.crt" >/dev/null 2>&1
ssh-keygen -q -t ed25519 -N "" -f "$smoke_directory/access-gateway-ssh-host-key"
cp "$smoke_directory/ca.crt" "$smoke_directory/access-gateway-tls/ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/control-plane-tls/ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/control-plane-tls/worker-ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/worker-tls/client-ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/target-worker-credentials/client-ca.crt"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-compose-docker-target-ca \
  -keyout "$smoke_directory/docker-target-ca.key" -out "$smoke_directory/docker-target-ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=host.docker.internal \
  -keyout "$smoke_directory/docker-target-server.key" -out "$smoke_directory/docker-target-server.csr" \
  -addext subjectAltName=DNS:host.docker.internal,IP:127.0.0.1 \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/docker-target-server.csr" \
  -CA "$smoke_directory/docker-target-ca.crt" -CAkey "$smoke_directory/docker-target-ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/docker-target-server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane-docker-target-client \
  -keyout "$smoke_directory/docker-target-credentials/docker-compose-target/key.pem" \
  -out "$smoke_directory/docker-target-client.csr" \
  -addext extendedKeyUsage=clientAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/docker-target-client.csr" \
  -CA "$smoke_directory/docker-target-ca.crt" -CAkey "$smoke_directory/docker-target-ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/docker-target-credentials/docker-compose-target/cert.pem" >/dev/null 2>&1
cp "$smoke_directory/docker-target-ca.crt" \
  "$smoke_directory/docker-target-credentials/docker-compose-target/ca.pem"
cp "$smoke_directory/docker-target-credentials/docker-compose-target/ca.pem" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore/ca.pem"
cp "$smoke_directory/docker-target-credentials/docker-compose-target/cert.pem" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore/cert.pem"
cp "$smoke_directory/docker-target-credentials/docker-compose-target/key.pem" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore/key.pem"
cp "$smoke_directory/docker-target-ca.crt" \
  "$smoke_directory/kubernetes-target-credentials/kubernetes-compose-target.ca.crt"
chmod 0444 "$smoke_directory"/access-gateway-tls/* \
  "$smoke_directory"/control-plane-tls/* "$smoke_directory"/worker-tls/*
chmod 0600 "$smoke_directory/access-gateway-ssh-host-key"

CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_RELEASE="$candidate_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_PROJECT="$project" \
CLOUD_AGENTS_COMPOSE_SMOKE_PLATFORM="$image_platform" \
CLOUD_AGENTS_COMPOSE_DOCKER_GATEWAY="$docker_gateway" \
CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST="$real_provider_test" \
  node <<'NODE'
const { createSign, generateKeyPairSync, randomBytes } = require("node:crypto");
const { chmodSync, writeFileSync } = require("node:fs");
const { isIP } = require("node:net");

const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const release = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_RELEASE;
const project = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PROJECT;
const platform = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PLATFORM;
const dockerGateway = process.env.CLOUD_AGENTS_COMPOSE_DOCKER_GATEWAY;
if (![state, release, project, platform, dockerGateway].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Compose smoke environment");
}
if (isIP(dockerGateway) !== 4) throw new Error("invalid Docker bridge gateway");
const deploy = `${state}/deployment/deploy`;
const issuer = "https://issuer.compose.test";
const audience = "https://api.compose.test";
const adminAudience = "https://admin-api.compose.test";
const kid = "compose-smoke-key";
const now = Math.floor(Date.now() / 1000);
const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const exported = publicKey.export({ format: "jwk" });
const jwk = { alg: "RS256", e: exported.e, key_ops: ["verify"], kid, kty: "RSA", n: exported.n, use: "sig" };
const auth = {
  issuer, audience, adminAudience, generation: 1, securityEpoch: 1, notBefore: now - 60, expiresAt: now + 7200,
  keys: [{ jwk, enabled: true, notBefore: now - 60, notAfter: now + 7200 }],
};
writeFileSync(`${state}/auth.json`, `${JSON.stringify(auth)}\n`);
writeFileSync(`${state}/auth-test-private-key.pem`, privateKey.export({ format: "pem", type: "pkcs8" }), { mode: 0o600 });
chmodSync(`${state}/auth-test-private-key.pem`, 0o600);
writeFileSync(`${state}/access-grant.key`, randomBytes(32));
const opensandboxApiKey = randomBytes(32).toString("hex");
writeFileSync(`${state}/opensandbox-api-key`, opensandboxApiKey);
writeFileSync(`${state}/opensandbox.toml`, `[server]
host="0.0.0.0"
eip="host.docker.internal"
port=8080
api_key="${opensandboxApiKey}"
[runtime]
type="docker"
execd_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd"
[docker]
network_mode="bridge"
host_ip="${dockerGateway}"
port_range_min=49530
port_range_max=49740
[egress]
image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a"
mode="dns+nft"
[storage]
allowed_host_paths=[]
[store]
type="sqlite"
path="/tmp/opensandbox.db"
`);
const baseClaims = {
  iss: issuer, sub: "user-compose-smoke", exp: now + 3500, iat: now - 10,
  client_id: "compose-smoke-client",
  "https://schemas.cloud-agents.dev/claims/security-epoch": 1,
  "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
  "https://schemas.cloud-agents.dev/claims/tenant-id": "tenant-compose-smoke",
  "https://schemas.cloud-agents.dev/claims/token-profile": "cloud-agents-access-token/v1",
};
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
const issueToken = (tokenId, tokenAudience, scopes) => {
  const claims = { ...baseClaims, aud: tokenAudience, jti: tokenId, scope: [...scopes].sort().join(" ") };
  const signingInput = `${encode({ alg: "RS256", kid, typ: "at+jwt" })}.${encode(claims)}`;
  const signature = createSign("RSA-SHA256").update(signingInput).end().sign(privateKey).toString("base64url");
  return `${signingInput}.${signature}`;
};
const adminScopes = [
  "audit.list", "environments.create", "environments.delete", "environments.get", "environment-profiles.list",
  "leases.act", "leases.get", "leases.list", "organizations.list", "profiles.act",
  "operations.list", "profiles.create", "profiles.get", "profiles.list", "projects.act", "projects.create",
  "network-policies.get", "network-policies.list", "network-policies.update", "projects.get", "quotas.get", "quotas.update", "releases.create", "releases.list", "sandboxes.act", "sandboxes.get", "sandboxes.list", "storage-policies.get", "storage-policies.list", "storage-policies.update", "targets.act", "targets.create", "targets.get", "targets.list", "workers.list",
  "snapshots.act", "snapshots.create", "snapshots.delete", "snapshots.get", "snapshots.list",
  "remote-worker-enrollments.act", "remote-worker-enrollments.create", "remote-worker-enrollments.get", "remote-worker-enrollments.list",
];
const userScopes = [
	"environment-quotas.get", "environments.create", "environments.delete", "environments.get", "environment-profiles.list",
	"organizations.list", "projects.act", "projects.create", "projects.get", "projects.list", "sandboxes.update", "tenants.get",
];
const adminToken = issueToken("compose-smoke-admin-token", adminAudience, adminScopes);
const adminDeniedToken = issueToken("compose-smoke-admin-denied-token", adminAudience, userScopes);
const userToken = issueToken("compose-smoke-user-token", audience, userScopes);
const remoteWorkerBootstrapToken = issueToken("compose-smoke-remote-worker-bootstrap", audience, ["projects.act", "remote-worker-bootstrap.act"]);
const admissionToken = randomBytes(24).toString("hex");
const kubernetesToken = randomBytes(24).toString("hex");
writeFileSync(`${state}/token`, `${adminToken}\n`);
writeFileSync(`${state}/admin-token`, `${adminToken}\n`);
writeFileSync(`${state}/admin-denied-token`, `${adminDeniedToken}\n`);
writeFileSync(`${state}/user-token`, `${userToken}\n`);
writeFileSync(`${state}/remote-worker-bootstrap-token`, `${remoteWorkerBootstrapToken}\n`);
writeFileSync(`${state}/admin-curl.conf`, `header = "Authorization: Bearer ${adminToken}"\n`);
writeFileSync(`${state}/admin-denied-curl.conf`, `header = "Authorization: Bearer ${adminDeniedToken}"\n`);
writeFileSync(`${state}/user-curl.conf`, `header = "Authorization: Bearer ${userToken}"\n`);
writeFileSync(`${state}/ssh-askpass.sh`, '#!/bin/sh\nprintf "%s\\n" "$CLOUD_AGENTS_GATEWAY_PASSWORD"\n');
writeFileSync(`${state}/runtime.env`, [
  "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent,pi,deepseek-harness",
  "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
  ...(process.env.CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST === "1"
    ? ["CLOUD_AGENT_CODEX_MANAGED_WRITE_RECEIPT_DELAY_MS=12000"]
    : []),
  "",
].join("\n"));
writeFileSync(`${state}/provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-worker-credentials/admission-token`, admissionToken);
writeFileSync(`${state}/kubernetes-target-credentials/kubernetes-compose-target.token`, `${kubernetesToken}\n`);
writeFileSync(`${state}/compose-target-override.yml`, `services:\n  access-gateway:\n    environment:\n      SSL_CERT_FILE: /run/cloud-agents/tls/ca.crt\n    extra_hosts:\n      - "host.docker.internal:host-gateway"\n  control-plane:\n    environment:\n      SSL_CERT_FILE: /run/cloud-agents/tls/ca.crt\n      CLOUD_AGENT_CODEX_MANAGED_WRITE_RECEIPT_DELAY_MS: "${process.env.CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST === "1" ? "12000" : ""}"\n    extra_hosts:\n      - "host.docker.internal:host-gateway"\n`);
writeFileSync(`${state}/docker-proxy.mjs`, [
  'import { chmodSync, readFileSync, writeFileSync } from "node:fs";',
  'import net from "node:net";',
  'import tls from "node:tls";',
  'const [socketPath, caPath, certPath, keyPath, portPath] = process.argv.slice(2);',
  'const upstreamTarget = socketPath.startsWith("tcp://") ? new URL(socketPath) : undefined;',
  'const server = tls.createServer({ ca: readFileSync(caPath), cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2", requestCert: true, rejectUnauthorized: true }, (client) => {',
  '  const upstream = upstreamTarget ? net.createConnection({ host: upstreamTarget.hostname, port: Number(upstreamTarget.port) }) : net.createConnection(socketPath);',
  '  const close = () => { client.destroy(); upstream.destroy(); };',
  '  client.on("error", close); upstream.on("error", close);',
  '  client.pipe(upstream); upstream.pipe(client);',
  '});',
  'server.on("tlsClientError", () => {});',
  'server.listen(0, "0.0.0.0", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });',
  'process.on("SIGTERM", () => process.exit(0));',
].join("\n") + "\n");
writeFileSync(`${state}/opensandbox-proxy.mjs`, [
  'import { chmodSync, readFileSync, writeFileSync } from "node:fs";',
  'import net from "node:net";',
  'import tls from "node:tls";',
  'const [certPath, keyPath, upstreamHostOrPort, upstreamPortOrPath, explicitPortPath] = process.argv.slice(2);',
  'const upstreamHost = explicitPortPath ? upstreamHostOrPort : "127.0.0.1";',
  'const upstreamPort = explicitPortPath ? upstreamPortOrPath : upstreamHostOrPort;',
  'const portPath = explicitPortPath ?? upstreamPortOrPath;',
  'const server = tls.createServer({ cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2" }, (client) => {',
  '  const upstream = net.createConnection({ host: upstreamHost, port: Number(upstreamPort) });',
  '  const close = () => { client.destroy(); upstream.destroy(); };',
  '  client.on("error", close); upstream.on("error", close);',
  '  client.pipe(upstream); upstream.pipe(client);',
  '});',
  'server.on("tlsClientError", () => {});',
  'server.listen(0, "0.0.0.0", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });',
  'process.on("SIGTERM", () => process.exit(0));',
].join("\n") + "\n");
writeFileSync(`${state}/kubernetes-api.mjs`, [
  'import { chmodSync, readFileSync, writeFileSync } from "node:fs";',
  'import https from "node:https";',
  'const [certPath, keyPath, tokenPath, portPath] = process.argv.slice(2);',
  'const token = readFileSync(tokenPath, "utf8").trimEnd();',
  'const server = https.createServer({ cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2" }, (request, response) => {',
  '  if (request.method !== "GET" || request.headers.authorization !== `Bearer ${token}`) { response.writeHead(401).end(); return; }',
  '  response.setHeader("content-type", "application/json");',
  '  const path = new URL(request.url, "https://kubernetes.invalid").pathname;',
  '  if (path === "/version") { response.end(JSON.stringify({ major: "1", minor: "34+", gitVersion: "v1.34.2", platform: "linux/arm64" })); return; }',
  '  if (["/apis/apps/v1/namespaces/cloud-agents-target/deployments", "/api/v1/namespaces/cloud-agents-target/services", "/api/v1/namespaces/cloud-agents-target/persistentvolumeclaims"].includes(path)) { response.end(JSON.stringify({ metadata: {}, items: [] })); return; }',
  '  response.writeHead(404).end();',
  '});',
  'server.listen(0, "0.0.0.0", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });',
  'process.on("SIGTERM", () => process.exit(0));',
].join("\n") + "\n");
const password = randomBytes(24).toString("hex");
const values = [
  `COMPOSE_PROJECT_NAME=${project}`,
  `CLOUD_AGENTS_RELEASE_DIR=${release}`,
  `CLOUD_AGENTS_DEPLOY_DIR=${deploy}`,
  `CLOUD_AGENTS_PLATFORM=${platform}`,
  "CLOUD_AGENTS_CONTROL_PLANE_BIND=127.0.0.1:",
  "CLOUD_AGENTS_USER_WEB_BIND=127.0.0.1:",
  "CLOUD_AGENTS_ADMIN_WEB_BIND=127.0.0.1:",
  "CLOUD_AGENTS_WORKER_BIND=127.0.0.1:",
  "CLOUD_AGENTS_POSTGRES_DB=cloud_agents",
  `CLOUD_AGENTS_POSTGRES_INSTALL_PASSWORD=${password}`,
  `CLOUD_AGENTS_MIGRATION_PASSWORD=${password}`,
  `CLOUD_AGENTS_RUNTIME_PASSWORD=${password}`,
  `CLOUD_AGENTS_TENANT_BOOTSTRAP_PASSWORD=${password}`,
  `CLOUD_AGENTS_BOOTSTRAP_DATABASE_URL=postgresql://cloud_agents_install_admin:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_MIGRATION_DATABASE_URL=postgresql://cloud_agents_migration:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_RUNTIME_DATABASE_URL=postgresql://cloud_agents_runtime_login:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_TENANT_BOOTSTRAP_DATABASE_URL=postgresql://cloud_agents_tenant_bootstrap:${password}@postgres:5432/cloud_agents`,
  "CLOUD_AGENTS_TENANT_UID=tenant-compose-smoke",
  "CLOUD_AGENTS_TENANT_NAME=tenant-compose-smoke",
  "CLOUD_AGENTS_TENANT_DISPLAY_NAME=Compose Smoke Tenant",
  "CLOUD_AGENTS_ORGANIZATION_UID=organization-compose-smoke",
  "CLOUD_AGENTS_ORGANIZATION_NAME=organization-compose-smoke",
  "CLOUD_AGENTS_ORGANIZATION_DISPLAY_NAME=Compose Smoke Organization",
  "CLOUD_AGENTS_ADMIN_SUBJECT_KIND=user",
  `CLOUD_AGENTS_ADMIN_SUBJECT_ISSUER=${issuer}`,
  "CLOUD_AGENTS_ADMIN_SUBJECT_VALUE=user-compose-smoke",
  "CLOUD_AGENTS_ADMIN_MEMBERSHIP_UID=membership-compose-admin",
  "CLOUD_AGENTS_ADMIN_MEMBERSHIP_NAME=membership-compose-admin",
  "CLOUD_AGENTS_ADMIN_ROLE_BINDING_UID=role-binding-compose-admin",
  "CLOUD_AGENTS_ADMIN_ROLE_BINDING_NAME=role-binding-compose-admin",
  "CLOUD_AGENTS_TENANT_AUDIT_FACT_UID=audit-compose-tenant",
  "CLOUD_AGENTS_MEMBERSHIP_AUDIT_FACT_UID=audit-compose-membership",
  "CLOUD_AGENTS_ROLE_BINDING_AUDIT_FACT_UID=audit-compose-role-binding",
  "CLOUD_AGENTS_BOOTSTRAP_REASON_CODE=compose-smoke",
  `CLOUD_AGENTS_AUTH_CONFIG=${state}/auth.json`,
  `CLOUD_AGENTS_ACCESS_GRANT_KEY_FILE=${state}/access-grant.key`,
  `CLOUD_AGENTS_RUNTIME_ENV_FILE=${state}/runtime.env`,
  `CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIR=${state}/provider-credentials`,
  `CLOUD_AGENTS_SNAPSHOT_DIR=${state}/snapshots`,
  `CLOUD_AGENTS_DOCKER_CREDENTIALS_DIR=${state}/docker-target-credentials`,
  `CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR=${state}/kubernetes-target-credentials`,
  `CLOUD_AGENTS_SSH_CREDENTIALS_DIR=${state}/ssh-target-credentials`,
  `CLOUD_AGENTS_CONTROL_PLANE_TLS_DIR=${state}/control-plane-tls`,
  `CLOUD_AGENTS_CONTROL_PLANE_CA=${state}/ca.crt`,
  `CLOUD_AGENTS_ACCESS_GATEWAY_TLS_DIR=${state}/access-gateway-tls`,
  `CLOUD_AGENTS_ACCESS_GATEWAY_SSH_HOST_KEY=${state}/access-gateway-ssh-host-key`,
  `CLOUD_AGENTS_WORKER_TLS_DIR=${state}/worker-tls`,
  "CLOUD_AGENTS_WORKER_ENDPOINT=https://worker:8091",
  "CLOUD_AGENTS_WORKER_SPIFFE_ID=spiffe://cloud-agents.compose/worker",
  `CLOUD_AGENTS_WORKER_CLIENT_CERT=${state}/control-plane-tls/worker-client.crt`,
  `CLOUD_AGENTS_WORKER_CLIENT_KEY=${state}/control-plane-tls/worker-client.key`,
  `CLOUD_AGENTS_WORKER_CA=${state}/control-plane-tls/worker-ca.crt`,
  `CLOUD_AGENTS_WORKSPACE_DIR=${state}/workspace`,
  `CLOUD_AGENTS_REMOTE_WORKER_CA_DIR=${state}/remote-worker-ca`,
  "CLOUD_AGENTS_REMOTE_WORKER_TRUST_DOMAIN=cloud-agents.compose.remote-worker",
  "CLOUD_AGENTS_WORKER_WORKSPACE_DIRECTORY=/workspace",
  "CLOUD_AGENTS_ACCESS_GATEWAY_BIND=127.0.0.1:",
  "CLOUD_AGENTS_ACCESS_GATEWAY_SSH_BIND=127.0.0.1:",
  "CLOUD_AGENTS_RUNTIME_MAX_SESSIONS=2",
  "CLOUD_AGENTS_ADMISSION_LEASE_ID=compose-smoke-lease",
  "CLOUD_AGENTS_ADMISSION_GENERATION=1",
  `CLOUD_AGENTS_ADMISSION_TOKEN=${admissionToken}`,
];
writeFileSync(`${state}/compose.env`, `${values.join("\n")}\n`);
const managedAgentPrefixes = [
  "CLOUD_AGENTS_RUNTIME_ENV_FILE=", "CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIR=", "CLOUD_AGENTS_SNAPSHOT_DIR=",
  "CLOUD_AGENTS_WORKER_", "CLOUD_AGENTS_WORKSPACE_DIR=", "CLOUD_AGENTS_RUNTIME_MAX_SESSIONS=",
  "CLOUD_AGENTS_ADMISSION_",
];
writeFileSync(`${state}/compose.no-agent.env`, `${values.filter((value) =>
  !managedAgentPrefixes.some((prefix) => value.startsWith(prefix))).join("\n")}\n`);
chmodSync(`${state}/auth.json`, 0o444);
chmodSync(`${state}/access-grant.key`, 0o600);
chmodSync(`${state}/opensandbox-api-key`, 0o600);
chmodSync(`${state}/opensandbox.toml`, 0o600);
chmodSync(`${state}/runtime.env`, 0o444);
chmodSync(`${state}/provider-credentials/tenant-compose-smoke.unavailable-provider.json`, 0o444);
chmodSync(`${state}/target-provider-credentials/tenant-compose-smoke.unavailable-provider.json`, 0o444);
chmodSync(`${state}/target-worker-credentials/admission-token`, 0o400);
chmodSync(`${state}/kubernetes-target-credentials/kubernetes-compose-target.token`, 0o400);
chmodSync(`${state}/docker-proxy.mjs`, 0o400);
chmodSync(`${state}/opensandbox-proxy.mjs`, 0o400);
chmodSync(`${state}/kubernetes-api.mjs`, 0o400);
chmodSync(`${state}/token`, 0o600);
chmodSync(`${state}/admin-token`, 0o600);
chmodSync(`${state}/admin-denied-token`, 0o600);
chmodSync(`${state}/user-token`, 0o600);
chmodSync(`${state}/remote-worker-bootstrap-token`, 0o600);
chmodSync(`${state}/admin-curl.conf`, 0o600);
chmodSync(`${state}/admin-denied-curl.conf`, 0o600);
chmodSync(`${state}/user-curl.conf`, 0o600);
chmodSync(`${state}/ssh-askpass.sh`, 0o700);
chmodSync(`${state}/compose.env`, 0o600);
chmodSync(`${state}/compose.no-agent.env`, 0o600);
NODE

chmod 0444 "$smoke_directory"/target-worker-credentials/server.* \
  "$smoke_directory"/target-worker-credentials/client-ca.crt \
  "$smoke_directory"/docker-target-credentials/docker-compose-target/*.pem \
  "$smoke_directory"/docker-target-credentials/docker-compose-target-restore/*.pem \
  "$smoke_directory"/kubernetes-target-credentials/kubernetes-compose-target.ca.crt

docker_proxy_port_file="$smoke_directory/docker-proxy.port"
node "$smoke_directory/docker-proxy.mjs" "$docker_socket" \
  "$smoke_directory/docker-target-ca.crt" "$smoke_directory/docker-target-server.crt" \
  "$smoke_directory/docker-target-server.key" "$docker_proxy_port_file" &
docker_proxy_pid=$!
attempt=0
while [ ! -s "$docker_proxy_port_file" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ] || ! kill -0 "$docker_proxy_pid" 2>/dev/null; then
    echo "Docker target mTLS proxy did not start" >&2
    exit 1
  fi
  sleep 0.1
done
docker_proxy_port=$(cat "$docker_proxy_port_file")
case "$docker_proxy_port" in
  '' | *[!0-9]*) echo "Docker target mTLS proxy returned an invalid port" >&2; exit 1 ;;
esac

kubernetes_api_port_file="$smoke_directory/kubernetes-api.port"
node "$smoke_directory/kubernetes-api.mjs" \
  "$smoke_directory/docker-target-server.crt" "$smoke_directory/docker-target-server.key" \
  "$smoke_directory/kubernetes-target-credentials/kubernetes-compose-target.token" \
  "$kubernetes_api_port_file" &
kubernetes_api_pid=$!
attempt=0
while [ ! -s "$kubernetes_api_port_file" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ] || ! kill -0 "$kubernetes_api_pid" 2>/dev/null; then
    echo "Kubernetes target API smoke server did not start" >&2
    exit 1
  fi
  sleep 0.1
done
kubernetes_api_port=$(cat "$kubernetes_api_port_file")
case "$kubernetes_api_port" in
  '' | *[!0-9]*) echo "Kubernetes target API smoke server returned an invalid port" >&2; exit 1 ;;
esac
test "$(curl --silent --show-error --fail \
  --cacert "$smoke_directory/docker-target-ca.crt" \
  --cert "$smoke_directory/docker-target-credentials/docker-compose-target/cert.pem" \
  --key "$smoke_directory/docker-target-credentials/docker-compose-target/key.pem" \
  "https://127.0.0.1:$docker_proxy_port/_ping")" = OK

compose_base config --quiet
compose_base config >"$smoke_directory/no-agent-config.yml"
if grep -Eq 'provider-credentials|CLOUD_AGENTS_PLATFORM_(WORKER|ADMISSION)|^[[:space:]]+worker:' "$smoke_directory/no-agent-config.yml"; then
  echo "default Compose configuration contains Managed Agent Runtime authority" >&2
  exit 1
fi
compose_base --profile bootstrap run --rm bootstrap >/dev/null
docker run -d --name "$opensandbox_container" \
  --network "${project}_default" --network-alias opensandbox \
  --add-host host.docker.internal:host-gateway \
  --label "cloud-agents.dev/test=$project" \
  -p 127.0.0.1::8080 \
  --mount "type=bind,src=$smoke_directory/opensandbox.toml,dst=/etc/opensandbox/config.toml,readonly" \
  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
  "$opensandbox_server_image" >/dev/null
opensandbox_endpoint=$(docker port "$opensandbox_container" 8080/tcp)
opensandbox_port=${opensandbox_endpoint##*:}
case "$opensandbox_port" in
  '' | *[!0-9]*) echo "Compose OpenSandbox target returned an invalid port" >&2; exit 1 ;;
esac
attempt=0
until curl --silent --show-error --fail "http://127.0.0.1:$opensandbox_port/health" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    docker logs "$opensandbox_container" >&2
    exit 1
  fi
  sleep 1
done
opensandbox_proxy_port_file="$smoke_directory/opensandbox-proxy.port"
node "$smoke_directory/opensandbox-proxy.mjs" \
  "$smoke_directory/opensandbox-proxy.crt" "$smoke_directory/opensandbox-proxy.key" \
  "$opensandbox_port" "$opensandbox_proxy_port_file" &
opensandbox_proxy_pid=$!
attempt=0
while [ ! -s "$opensandbox_proxy_port_file" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ] || ! kill -0 "$opensandbox_proxy_pid" 2>/dev/null; then
    echo "OpenSandbox TLS proxy did not start" >&2
    exit 1
  fi
  sleep 0.1
done
opensandbox_proxy_port=$(cat "$opensandbox_proxy_port_file")
case "$opensandbox_proxy_port" in
  '' | *[!0-9]*) echo "OpenSandbox TLS proxy returned an invalid port" >&2; exit 1 ;;
esac
CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_PORT="$opensandbox_proxy_port" \
CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_KEY_FILE="$smoke_directory/opensandbox-api-key" \
CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL="$smoke_directory/docker-target-credentials/docker-compose-target/opensandbox.json" \
  node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
const apiKey = readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_KEY_FILE, "utf8");
writeFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL,
  `${JSON.stringify({ endpoint: `https://host.docker.internal:${process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_PORT}`, apiKey })}\n`,
  { mode: 0o444 });
chmodSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL, 0o444);
NODE
compose_base --profile tenant-bootstrap run --rm tenant-bootstrap >/dev/null
compose_base up -d --build >/dev/null
for service in postgres control-plane access-gateway admin-web user-web; do
  compose_base ps --services --status running | grep -Fx "$service" >/dev/null || {
    echo "default Compose service did not start: $service" >&2
    exit 1
  }
done
if compose_base ps --services --status running | grep -Fx worker >/dev/null; then
  echo "default Compose unexpectedly started the Managed Agent Worker" >&2
  exit 1
fi
base_endpoint=$(compose_base port control-plane 8080)
attempt=0
until curl --silent --show-error --fail --noproxy '*' --cacert "$smoke_directory/ca.crt" "https://$base_endpoint/readyz" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    compose_base logs --no-color --tail=200 control-plane migrate postgres >&2
    exit 1
  fi
  sleep 1
done
for service_port in user-web:4173 admin-web:4174; do
  service=${service_port%:*}
  port=${service_port##*:}
  web_endpoint=$(compose_base port "$service" "$port")
  curl --silent --show-error --fail --noproxy '*' "http://$web_endpoint/" >"$smoke_directory/no-agent-$service.html"
  asset=$(sed -n 's/.*src="\([^"]*\.js\)".*/\1/p' "$smoke_directory/no-agent-$service.html")
  if [ -z "$asset" ] || ! curl --silent --show-error --fail --noproxy '*' "http://$web_endpoint$asset" >/dev/null; then
    echo "default Compose $service did not serve its packaged application assets" >&2
    cat "$smoke_directory/no-agent-$service.html" >&2
    compose_base exec -T "$service" sh -c 'find /opt/cloud-agents/web/dist -maxdepth 2 -type f -print; sed -n "1,20p" /opt/cloud-agents/web/dist/index.html' >&2 || true
    exit 1
  fi
done
compose up -d --build >/dev/null

endpoint=
admin_web_endpoint=
gateway_endpoint=
gateway_ssh_port=
wait_ready() {
  endpoint=$(compose port control-plane 8080)
  if [ -z "$endpoint" ]; then
    echo "Compose Control Plane port is unavailable" >&2
    exit 1
  fi
  attempt=0
  until curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" "https://$endpoint/readyz" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      compose logs --no-color --tail=200 worker control-plane migrate postgres >&2
      exit 1
    fi
    sleep 1
  done
}
wait_gateway() {
  gateway_endpoint=$(compose port access-gateway 8090)
  gateway_ssh_endpoint=$(compose port access-gateway 2222)
  gateway_ssh_port=${gateway_ssh_endpoint##*:}
  if [ -z "$gateway_endpoint" ] || [ -z "$gateway_ssh_port" ]; then
    echo "Compose Access Gateway ports are unavailable" >&2
    exit 1
  fi
  attempt=0
  until curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" \
    "https://$gateway_endpoint/healthz" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      compose logs --no-color --tail=200 access-gateway access-gateway-ssh-key postgres >&2
      exit 1
    fi
    sleep 1
  done
}
wait_admin_web() {
  admin_web_endpoint=$(compose port admin-web 4174)
  case "$admin_web_endpoint" in
    0.0.0.0:*) admin_web_endpoint=127.0.0.1:${admin_web_endpoint##*:} ;;
    \[::\]:*) admin_web_endpoint=127.0.0.1:${admin_web_endpoint##*:} ;;
  esac
  if [ -z "$admin_web_endpoint" ]; then
    echo "Compose Admin Web port is unavailable" >&2
    exit 1
  fi
  attempt=0
  until curl --silent --show-error --fail "http://$admin_web_endpoint/healthz" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      compose logs --no-color --tail=200 admin-web control-plane >&2
      exit 1
    fi
    sleep 1
  done
}
wait_user_web() {
  user_web_endpoint=$(compose port user-web 4173)
  case "$user_web_endpoint" in
    0.0.0.0:*) user_web_endpoint=127.0.0.1:${user_web_endpoint##*:} ;;
    \[::\]:*) user_web_endpoint=127.0.0.1:${user_web_endpoint##*:} ;;
  esac
  if [ -z "$user_web_endpoint" ]; then
    echo "Compose User Web port is unavailable" >&2
    exit 1
  fi
  attempt=0
  until curl --silent --show-error --fail "http://$user_web_endpoint/healthz" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      compose logs --no-color --tail=200 user-web control-plane >&2
      exit 1
    fi
    sleep 1
  done
}
wait_ready
wait_gateway
wait_user_web
wait_admin_web
gateway_container=$(compose ps -q access-gateway)
test "$(docker inspect --format '{{.Config.User}}' "$gateway_container")" = "65532:65532"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$gateway_container")" = true
test "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$gateway_container")" = '["ALL"]'
case "$(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$gateway_container")" in
  *'/var/run/docker.sock'*) echo "Access Gateway received direct Docker authority" >&2; exit 1 ;;
esac
user_web_container=$(compose ps -q user-web)
test "$(docker inspect --format '{{.Config.User}}' "$user_web_container")" = "1000:1000"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$user_web_container")" = true
test "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$user_web_container")" = '["ALL"]'
case "$(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$user_web_container")" in
  *'/var/run/docker.sock'* | *'credentials'*) echo "User Web received infrastructure authority" >&2; exit 1 ;;
esac
admin_web_container=$(compose ps -q admin-web)
test "$(docker inspect --format '{{.Config.User}}' "$admin_web_container")" = "1000:1000"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$admin_web_container")" = true
test "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$admin_web_container")" = '["ALL"]'
case "$(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$admin_web_container")" in
  *'/var/run/docker.sock'* | *'credentials'*) echo "Admin Web received infrastructure authority" >&2; exit 1 ;;
esac
cloud_agentsctl() {
  "$cli" --endpoint "https://$endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/token" --tenant tenant-compose-smoke "$@"
}
cloud_agentsctl_user() {
  "$cli" --endpoint "https://$endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/user-token" --tenant tenant-compose-smoke "$@"
}
cloud_agentsctl_gateway() {
  "$cli" --endpoint "https://$gateway_endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/gateway-grant-token" --tenant tenant-compose-smoke "$@"
}
control_plane_api() {
  auth_config=$1
  method=$2
  path=$3
  request_id=$4
  shift 4
  curl --silent --show-error --fail-with-body --cacert "$smoke_directory/ca.crt" \
    --config "$auth_config" --request "$method" --header "X-Request-ID: $request_id" \
    --header "Content-Type: application/json" "$@" "https://$endpoint$path"
}
expect_snapshot_backend_rejected() {
  snapshot_id=$1
  expected_version=$2
  restore_path=$3
  restore_body=$4
  output_file=$5
  postgres_container=$(compose ps -q postgres)
  printf '%s\n' "$postgres_container" | grep -Eq '^[0-9a-f]{64}$' || {
    echo "Postgres container must be an exact Docker container id" >&2
    return 1
  }
  postgres_snapshot_query() {
    sql=$1
    printf '%s\n' "$sql" | docker exec -i "$postgres_container" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
      -v ON_ERROR_STOP=1 -v tenant=tenant-compose-smoke -v project="$project_id" -v snapshot="$snapshot_id"
  }
  original_backend=$(postgres_snapshot_query \
    "SELECT backend FROM cloud_agents.workspace_snapshots WHERE tenant_id = :'tenant' AND project_uid = :'project' AND snapshot_uid = :'snapshot';")
  if [ "$original_backend" != portable-tar-v1 ]; then
    echo "expected portable snapshot backend, got '$original_backend'" >&2
    return 1
  fi
  postgres_snapshot_query \
    "UPDATE cloud_agents.workspace_snapshots SET backend = 'docker-volume-v1' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND snapshot_uid = :'snapshot';" >/dev/null
  set +e
  http_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
    --config "$smoke_directory/admin-curl.conf" --request POST \
    --header "X-Request-ID: compose-snapshot-version-negative" \
    --header "Idempotency-Key: compose-snapshot-version-negative" \
    --header "Content-Type: application/json" --data "$restore_body" \
    --output "$output_file" --write-out '%{http_code}' "https://$endpoint$restore_path")
  curl_status=$?
  set -e
  postgres_snapshot_query \
    "UPDATE cloud_agents.workspace_snapshots SET backend = 'portable-tar-v1' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND snapshot_uid = :'snapshot';" >/dev/null
  if [ "$curl_status" -ne 0 ] || [ "$http_status" -ne 409 ] || ! grep -Eq '"code":"(RESOURCE_CONFLICT|resource_conflict)"' "$output_file"; then
    echo "incompatible snapshot backend was accepted: curl=$curl_status http=$http_status" >&2
    cat "$output_file" >&2
    return 1
  fi
  printf 'snapshot_version_negative=passed backend=docker-volume-v1\n'
}
submit_foundation_operation_api() {
  auth_config=$1
  path=$2
  request_id=$3
  idempotency_key=$4
  request_body=$5
  output_file=$6
  attempt=1
  while :; do
    http_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
      --config "$auth_config" --request POST \
      --header "X-Request-ID: $request_id" --header "Idempotency-Key: $idempotency_key" \
      --header "Content-Type: application/json" --data "$request_body" \
      --output "$output_file" --write-out '%{http_code}' "https://$endpoint$path")
    if [ "$http_status" -eq 202 ]; then
      return
    fi
    if [ "$http_status" -ne 409 ] || ! grep -Eq '"code":"(RESOURCE_CONFLICT|resource_conflict)"' "$output_file" || [ "$attempt" -ge 5 ]; then
      echo "Compose Foundation operation $request_id failed with HTTP $http_status" >&2
      cat "$output_file" >&2
      return 1
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
}
create_user_environment_api() {
  output_file=$1
  request_id=$2
  idempotency_key=$3
  request_body=$4
  attempt=1
  while :; do
    http_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
      --config "$smoke_directory/user-curl.conf" --request POST \
      --header "X-Request-ID: $request_id" --header "Idempotency-Key: $idempotency_key" \
      --header "Content-Type: application/json" --data "$request_body" \
      --output "$output_file" --write-out '%{http_code}' \
      "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/environments")
    if [ "$http_status" -eq 201 ]; then
      return
    fi
    if [ "$http_status" -ne 409 ] || ! grep -q '"code":"LEASE_CONFLICT"' "$output_file" || [ "$attempt" -ge 3 ]; then
      echo "Compose User Environment request $request_id failed with HTTP $http_status" >&2
      cat "$output_file" >&2
      return 1
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
}
admin_lease_release_operation() {
  action=$1
  preview_path=$2
  request_id=$3
  idempotency_key=$4
  request_body_file=$5
  output_file=$6
  retry_preview_file=$7
  attempt=1
  while :; do
    status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
      --config "$smoke_directory/admin-curl.conf" --request POST \
      --header "X-Request-ID: $request_id" --header "Idempotency-Key: $idempotency_key" \
      --header "Content-Type: application/json" --data-binary "@$request_body_file" \
      --output "$output_file" --write-out '%{http_code}' \
      "https://$endpoint$lease_release_path:$action")
    if [ "$status" -eq 200 ]; then
      return
    fi
    if [ "$status" -ne 409 ] || ! grep -q '"code":"LEASE_RESOURCE_VERSION_CONFLICT"' "$output_file" || [ "$attempt" -ge 3 ]; then
      echo "Compose Worker $action failed with HTTP $status" >&2
      cat "$output_file" >&2
      return 1
    fi
    echo "Compose Worker $action hit a concurrent Lease update; refreshing its authority preview" >&2
    sleep 1
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$preview_path" \
      "$request_id-retry-preview-$attempt" >"$retry_preview_file"
    CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$retry_preview_file" CLOUD_AGENTS_COMPOSE_ACTION="$action" node <<'NODE' >"$request_body_file"
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE, "utf8"));
const spec = value.spec;
if (spec?.action !== process.env.CLOUD_AGENTS_COMPOSE_ACTION ||
    spec.expectedResourceVersion !== value.metadata?.resourceVersion ||
    !/^sha256:[0-9a-f]{64}$/.test(spec?.impactDigest)) {
  throw new Error("refreshed Admin Lease authority preview is invalid");
}
process.stdout.write(JSON.stringify({
  releaseDigest: spec.targetReleaseDigest,
  expectedGeneration: spec.expectedGeneration,
  expectedResourceVersion: spec.expectedResourceVersion,
  impactDigest: spec.impactDigest,
}));
NODE
    attempt=$((attempt + 1))
  done
}

docker run -d --name "$registry_container" -p 127.0.0.1::5000 registry:2 >/dev/null
registry_endpoint=$(docker port "$registry_container" 5000/tcp)
registry_port=${registry_endpoint##*:}
case "$registry_port" in
  '' | *[!0-9]*) echo "local Worker registry returned an invalid port" >&2; exit 1 ;;
esac
attempt=0
until curl --silent --show-error --fail "http://127.0.0.1:$registry_port/v2/" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    docker logs "$registry_container" >&2
    exit 1
  fi
  sleep 1
done
worker_repository="localhost:$registry_port/cloud-agents/worker"
worker_image=$(compose images -q worker)
if [ -z "$worker_image" ]; then
  echo "Compose Worker image is unavailable" >&2
  exit 1
fi
docker tag "$worker_image" "$worker_repository:smoke"
docker push "$worker_repository:smoke" >/dev/null
worker_reference=$(docker image inspect "$worker_repository:smoke" --format '{{json .RepoDigests}}' |
  node -e 'const fs=require("node:fs");const values=JSON.parse(fs.readFileSync(0,"utf8"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
worker_release_digest=${worker_reference##*@}
case "$worker_release_digest" in
  sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "pushed Worker image returned an invalid digest" >&2; exit 1 ;;
esac
docker create --name "${project}-worker-upgrade-seed" "$worker_image" >/dev/null
docker commit --change 'LABEL cloud-agents.dev.compose-release=upgrade' \
  "${project}-worker-upgrade-seed" "$worker_repository:upgrade" >/dev/null
docker rm "${project}-worker-upgrade-seed" >/dev/null
docker push "$worker_repository:upgrade" >/dev/null
worker_upgrade_reference=$(docker image inspect "$worker_repository:upgrade" --format '{{json .RepoDigests}}' |
  node -e 'const fs=require("node:fs");const values=JSON.parse(fs.readFileSync(0,"utf8"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
worker_upgrade_release_digest=${worker_upgrade_reference##*@}
case "$worker_upgrade_release_digest" in
  sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "pushed upgrade Worker image returned an invalid digest" >&2; exit 1 ;;
esac
if [ "$worker_upgrade_release_digest" = "$worker_release_digest" ]; then
  echo "Compose upgrade Worker image did not produce a distinct digest" >&2
  exit 1
fi
if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
  kubernetes_execd_seed="${project}-kubernetes-execd:fixed"
  kubernetes_egress_seed="${project}-kubernetes-egress:fixed"
  kubernetes_execd_kind_seed="docker.io/library/$kubernetes_execd_seed"
  kubernetes_egress_kind_seed="docker.io/library/$kubernetes_egress_seed"
  docker tag "$kubernetes_execd_image" "$kubernetes_execd_seed"
  docker tag "$kubernetes_egress_image" "$kubernetes_egress_seed"
  kind load docker-image "$worker_repository:smoke" "$worker_repository:upgrade" \
    "$kubernetes_execd_seed" "$kubernetes_egress_seed" --name "$kind_cluster" >/dev/null
  for kind_node in $(kind get nodes --name "$kind_cluster"); do
    docker exec "$kind_node" ctr -n k8s.io images tag \
      "$worker_repository:smoke" "$worker_repository@$worker_release_digest" >/dev/null
    docker exec "$kind_node" ctr -n k8s.io images tag \
      "$worker_repository:upgrade" "$worker_repository@$worker_upgrade_release_digest" >/dev/null
    docker exec "$kind_node" ctr -n k8s.io images tag --force \
      "$kubernetes_execd_kind_seed" "$kubernetes_execd_image" >/dev/null
    docker exec "$kind_node" ctr -n k8s.io images tag --force \
      "$kubernetes_egress_kind_seed" "$kubernetes_egress_image" >/dev/null
  done
  docker image rm "$kubernetes_execd_seed" "$kubernetes_egress_seed" >/dev/null
fi

if [ "$kubernetes_runtime" -eq 1 ]; then
  for opensandbox_crd in batchsandboxes.sandbox.opensandbox.io pools.sandbox.opensandbox.io sandboxsnapshots.sandbox.opensandbox.io; do
    test "$(kubernetes_ctl get crd "$opensandbox_crd" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/version}')" = 0.2.0
  done
  test "$(kubernetes_ctl get clusterrole opensandbox-manager-role -o jsonpath='{.metadata.labels.app\.kubernetes\.io/version}')" = 0.2.0
  kubernetes_ctl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: Namespace
    metadata:
      name: $kubernetes_runtime_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: Namespace
    metadata:
      name: $kubernetes_operator_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: control-plane
      namespace: $kubernetes_runtime_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: opensandbox-server
      namespace: $kubernetes_runtime_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: opensandbox-controller
      namespace: $kubernetes_operator_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: Role
    metadata:
      name: foundation-workspace
      namespace: $kubernetes_runtime_namespace
      labels:
        cloud-agents.dev/test-run: $project
    rules:
      - apiGroups: ["apps"]
        resources: ["deployments"]
        verbs: ["get", "list", "create", "patch", "delete"]
      - apiGroups: [""]
        resources: ["services", "persistentvolumeclaims"]
        verbs: ["get", "list", "create", "patch", "delete"]
      - apiGroups: [""]
        resources: ["pods"]
        verbs: ["get", "create", "delete"]
      - apiGroups: [""]
        resources: ["pods/exec"]
        verbs: ["get", "create"]
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: RoleBinding
    metadata:
      name: foundation-workspace
      namespace: $kubernetes_runtime_namespace
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: control-plane
        namespace: $kubernetes_runtime_namespace
      - kind: ServiceAccount
        name: opensandbox-server
        namespace: $kubernetes_runtime_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: Role
      name: foundation-workspace
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: $kubernetes_version_role
      labels:
        cloud-agents.dev/test-run: $project
    rules:
      - nonResourceURLs: ["/version"]
        verbs: ["get"]
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: $kubernetes_version_binding
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: control-plane
        namespace: $kubernetes_runtime_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: $kubernetes_version_role
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: $kubernetes_manager_binding
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: opensandbox-server
        namespace: $kubernetes_runtime_namespace
      - kind: ServiceAccount
        name: opensandbox-controller
        namespace: $kubernetes_operator_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: opensandbox-manager-role
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: opensandbox-controller
      namespace: $kubernetes_operator_namespace
      labels:
        cloud-agents.dev/test-run: $project
    spec:
      replicas: 1
      selector:
        matchLabels:
          cloud-agents.dev/test-run: $project
      template:
        metadata:
          labels:
            cloud-agents.dev/test-run: $project
        spec:
          serviceAccountName: opensandbox-controller
          securityContext:
            runAsNonRoot: true
            seccompProfile:
              type: RuntimeDefault
          containers:
            - name: manager
              image: $kubernetes_controller_image
              imagePullPolicy: IfNotPresent
              command: ["/workspace/server"]
              args: ["--health-probe-bind-address=:8081", "--zap-log-level=info", "--kube-client-qps=100", "--kube-client-burst=200"]
              securityContext:
                allowPrivilegeEscalation: false
                capabilities:
                  drop: ["ALL"]
EOF
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_ctl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: Namespace
    metadata:
      name: $kubernetes_destination_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: control-plane
      namespace: $kubernetes_destination_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: opensandbox-server
      namespace: $kubernetes_destination_namespace
      labels:
        cloud-agents.dev/test-run: $project
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: Role
    metadata:
      name: foundation-workspace
      namespace: $kubernetes_destination_namespace
      labels:
        cloud-agents.dev/test-run: $project
    rules:
      - apiGroups: ["apps"]
        resources: ["deployments"]
        verbs: ["get", "list", "create", "patch", "delete"]
      - apiGroups: [""]
        resources: ["services", "persistentvolumeclaims"]
        verbs: ["get", "list", "create", "patch", "delete"]
      - apiGroups: [""]
        resources: ["pods"]
        verbs: ["get", "create", "delete"]
      - apiGroups: [""]
        resources: ["pods/exec"]
        verbs: ["get", "create"]
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: RoleBinding
    metadata:
      name: foundation-workspace
      namespace: $kubernetes_destination_namespace
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: control-plane
        namespace: $kubernetes_destination_namespace
      - kind: ServiceAccount
        name: opensandbox-server
        namespace: $kubernetes_destination_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: Role
      name: foundation-workspace
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: $kubernetes_destination_version_binding
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: control-plane
        namespace: $kubernetes_destination_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: $kubernetes_version_role
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: $kubernetes_destination_manager_binding
      labels:
        cloud-agents.dev/test-run: $project
    subjects:
      - kind: ServiceAccount
        name: opensandbox-server
        namespace: $kubernetes_destination_namespace
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: opensandbox-manager-role
EOF
  fi
  kubernetes_ctl -n "$kubernetes_operator_namespace" rollout status deployment/opensandbox-controller --timeout=180s >/dev/null
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_ctl -n "$kubernetes_operator_namespace" patch deployment opensandbox-controller --type merge \
      -p "{\"spec\":{\"template\":{\"spec\":{\"nodeSelector\":{\"kubernetes.io/hostname\":\"$kubernetes_destination_node\"}}}}}" >/dev/null
    kubernetes_ctl -n "$kubernetes_operator_namespace" rollout status deployment/opensandbox-controller --timeout=180s >/dev/null
  fi

  kubernetes_control_plane_token=$(kubernetes_ctl -n "$kubernetes_runtime_namespace" create token control-plane --duration=1h)
  kubernetes_opensandbox_token=$(kubernetes_ctl -n "$kubernetes_runtime_namespace" create token opensandbox-server --duration=1h)
  kubernetes_cluster_json=$(kubernetes_ctl config view --raw --minify --flatten -o json)
  kubernetes_target_endpoint=$(printf '%s' "$kubernetes_cluster_json" | node -e 'const fs=require("node:fs");const v=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(v.clusters[0].cluster.server)')
  kubernetes_ca_data=$(printf '%s' "$kubernetes_cluster_json" | node -e 'const fs=require("node:fs");const v=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(v.clusters[0].cluster["certificate-authority-data"])')
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    docker network connect "$kind_network" "$(compose ps -q control-plane)"
    kubernetes_target_endpoint="https://$kind_control_plane:6443"
  fi
  printf '%s' "$kubernetes_ca_data" | openssl base64 -d -A >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.ca.crt"
  CLOUD_AGENTS_KUBERNETES_ENDPOINT="$kubernetes_target_endpoint" \
  CLOUD_AGENTS_KUBERNETES_CA_DATA="$kubernetes_ca_data" \
  CLOUD_AGENTS_KUBERNETES_TOKEN="$kubernetes_opensandbox_token" \
  CLOUD_AGENTS_KUBERNETES_NAMESPACE="$kubernetes_runtime_namespace" \
  CLOUD_AGENTS_KUBERNETES_STATE="$smoke_directory" node <<'NODE'
const { writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_KUBERNETES_STATE;
writeFileSync(`${state}/kubernetes-opensandbox-kubeconfig.json`, `${JSON.stringify({
  apiVersion: "v1", kind: "Config",
  clusters: [{ name: "target", cluster: { server: process.env.CLOUD_AGENTS_KUBERNETES_ENDPOINT, "certificate-authority-data": process.env.CLOUD_AGENTS_KUBERNETES_CA_DATA } }],
  users: [{ name: "server", user: { token: process.env.CLOUD_AGENTS_KUBERNETES_TOKEN } }],
  contexts: [{ name: "target", context: { cluster: "target", user: "server", namespace: process.env.CLOUD_AGENTS_KUBERNETES_NAMESPACE } }],
  "current-context": "target",
})}\n`);
NODE
  kubernetes_opensandbox_api_key=$(openssl rand -hex 24)
  cat >"$smoke_directory/kubernetes-opensandbox.toml" <<EOF
[server]
host="0.0.0.0"
port=8080
api_key="$kubernetes_opensandbox_api_key"
max_sandbox_timeout_seconds=86400
[log]
level="INFO"
[runtime]
type="kubernetes"
execd_image="$kubernetes_execd_image"
[storage]
allowed_host_paths=[]
volume_default_size="20Gi"
[kubernetes]
kubeconfig_path="/run/kube/config"
namespace="$kubernetes_runtime_namespace"
informer_enabled=false
workload_provider="batchsandbox"
image_pull_policy="IfNotPresent"
sandbox_create_timeout_seconds=180
batchsandbox_template_file="/etc/opensandbox/batchsandbox-template.yaml"
[ingress]
mode="direct"
[egress]
image="$kubernetes_egress_image"
mode="dns+nft"
[renew_intent]
enabled=false
redis.enabled=false
[store]
type="sqlite"
path="/tmp/opensandbox.db"
EOF
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    printf 'metadata:\nspec:\n  replicas: 1\n  template:\n    spec:\n      restartPolicy: Never\n      nodeSelector:\n        kubernetes.io/hostname: %s\n' "$kubernetes_source_node" >"$smoke_directory/kubernetes-batchsandbox-template.yaml"
  else
    printf 'metadata:\nspec:\n  replicas: 1\n  template:\n    spec:\n      restartPolicy: Never\n' >"$smoke_directory/kubernetes-batchsandbox-template.yaml"
  fi
  chmod 0600 "$smoke_directory/kubernetes-opensandbox-kubeconfig.json" "$smoke_directory/kubernetes-opensandbox.toml" "$smoke_directory/kubernetes-batchsandbox-template.yaml"
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_opensandbox_upstream_host=$(docker inspect "$kubernetes_source_node" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
    case "$kubernetes_opensandbox_upstream_host" in
      '' | *' '*) echo "Kubernetes source node returned an invalid Docker address" >&2; exit 1 ;;
    esac
    docker run -d --name "$kubernetes_opensandbox_container" --label "cloud-agents.dev/test=$project" \
      --network "container:$kubernetes_source_node" \
      --mount "type=bind,src=$smoke_directory/kubernetes-opensandbox.toml,dst=/etc/opensandbox/config.toml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-batchsandbox-template.yaml,dst=/etc/opensandbox/batchsandbox-template.yaml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-opensandbox-kubeconfig.json,dst=/run/kube/config,readonly" \
      "$opensandbox_server_image" >/dev/null
    kubernetes_opensandbox_port=8080
  else
    docker run -d --name "$kubernetes_opensandbox_container" --label "cloud-agents.dev/test=$project" \
      --network "${project}_default" -p 127.0.0.1::8080 \
      --mount "type=bind,src=$smoke_directory/kubernetes-opensandbox.toml,dst=/etc/opensandbox/config.toml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-batchsandbox-template.yaml,dst=/etc/opensandbox/batchsandbox-template.yaml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-opensandbox-kubeconfig.json,dst=/run/kube/config,readonly" \
      "$opensandbox_server_image" >/dev/null
    kubernetes_opensandbox_endpoint=$(docker port "$kubernetes_opensandbox_container" 8080/tcp)
    kubernetes_opensandbox_upstream_host=127.0.0.1
    kubernetes_opensandbox_port=${kubernetes_opensandbox_endpoint##*:}
  fi
  attempt=0
  until curl --noproxy '*' --silent --show-error --fail "http://$kubernetes_opensandbox_upstream_host:$kubernetes_opensandbox_port/health" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      docker logs "$kubernetes_opensandbox_container" >&2
      exit 1
    fi
    sleep 1
  done
  kubernetes_opensandbox_proxy_port_file="$smoke_directory/kubernetes-opensandbox-proxy.port"
  node "$smoke_directory/opensandbox-proxy.mjs" \
    "$smoke_directory/opensandbox-proxy.crt" "$smoke_directory/opensandbox-proxy.key" \
    "$kubernetes_opensandbox_upstream_host" "$kubernetes_opensandbox_port" "$kubernetes_opensandbox_proxy_port_file" &
  kubernetes_opensandbox_proxy_pid=$!
  attempt=0
  while [ ! -s "$kubernetes_opensandbox_proxy_port_file" ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 50 ] || ! kill -0 "$kubernetes_opensandbox_proxy_pid" 2>/dev/null; then
      echo "Kubernetes OpenSandbox TLS proxy did not start" >&2
      exit 1
    fi
    sleep 0.1
  done
  kubernetes_opensandbox_proxy_port=$(cat "$kubernetes_opensandbox_proxy_port_file")
  case "$kubernetes_opensandbox_proxy_port" in
    '' | *[!0-9]*) echo "Kubernetes OpenSandbox TLS proxy returned an invalid port" >&2; exit 1 ;;
  esac
  printf '%s\n' "$kubernetes_control_plane_token" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.token"
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    printf '{"namespace":"%s","nodeName":"%s"}\n' "$kubernetes_runtime_namespace" "$kubernetes_source_node" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.foundation.json"
  else
    printf '{"namespace":"%s"}\n' "$kubernetes_runtime_namespace" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.foundation.json"
  fi
  printf '{"namespace":"%s","workerImageRepository":"%s","workerCredentialSecretRef":"cloud-agents-worker-target","workerSpiffeId":"spiffe://cloud-agents.compose/worker-target","workerServerName":"worker-target.example"}\n' \
    "$kubernetes_runtime_namespace" "$worker_repository" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.deployment.json"
  printf '{"endpoint":"https://host.docker.internal:%s","apiKey":"%s"}\n' "$kubernetes_opensandbox_proxy_port" "$kubernetes_opensandbox_api_key" \
    >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.opensandbox.json"
  chmod 0444 "$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.ca.crt" \
    "$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.token" \
    "$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.foundation.json" \
    "$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.deployment.json" \
    "$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.opensandbox.json"

  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_destination_control_plane_token=$(kubernetes_ctl -n "$kubernetes_destination_namespace" create token control-plane --duration=1h)
    kubernetes_destination_opensandbox_token=$(kubernetes_ctl -n "$kubernetes_destination_namespace" create token opensandbox-server --duration=1h)
    CLOUD_AGENTS_KUBERNETES_ENDPOINT="$kubernetes_target_endpoint" \
    CLOUD_AGENTS_KUBERNETES_CA_DATA="$kubernetes_ca_data" \
    CLOUD_AGENTS_KUBERNETES_TOKEN="$kubernetes_destination_opensandbox_token" \
    CLOUD_AGENTS_KUBERNETES_NAMESPACE="$kubernetes_destination_namespace" \
    CLOUD_AGENTS_KUBERNETES_STATE="$smoke_directory" node <<'NODE'
const { writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_KUBERNETES_STATE;
writeFileSync(`${state}/kubernetes-destination-opensandbox-kubeconfig.json`, `${JSON.stringify({
  apiVersion: "v1", kind: "Config",
  clusters: [{ name: "target", cluster: { server: process.env.CLOUD_AGENTS_KUBERNETES_ENDPOINT, "certificate-authority-data": process.env.CLOUD_AGENTS_KUBERNETES_CA_DATA } }],
  users: [{ name: "server", user: { token: process.env.CLOUD_AGENTS_KUBERNETES_TOKEN } }],
  contexts: [{ name: "target", context: { cluster: "target", user: "server", namespace: process.env.CLOUD_AGENTS_KUBERNETES_NAMESPACE } }],
  "current-context": "target",
})}\n`);
NODE
    kubernetes_destination_opensandbox_api_key=$(openssl rand -hex 24)
    cat >"$smoke_directory/kubernetes-destination-opensandbox.toml" <<EOF
[server]
host="0.0.0.0"
port=8080
api_key="$kubernetes_destination_opensandbox_api_key"
max_sandbox_timeout_seconds=86400
[log]
level="INFO"
[runtime]
type="kubernetes"
execd_image="$kubernetes_execd_image"
[storage]
allowed_host_paths=[]
volume_default_size="20Gi"
[kubernetes]
kubeconfig_path="/run/kube/config"
namespace="$kubernetes_destination_namespace"
informer_enabled=false
workload_provider="batchsandbox"
image_pull_policy="IfNotPresent"
sandbox_create_timeout_seconds=180
batchsandbox_template_file="/etc/opensandbox/batchsandbox-template.yaml"
[ingress]
mode="direct"
[egress]
image="$kubernetes_egress_image"
mode="dns+nft"
[renew_intent]
enabled=false
redis.enabled=false
[store]
type="sqlite"
path="/tmp/opensandbox.db"
EOF
    printf 'metadata:\nspec:\n  replicas: 1\n  template:\n    spec:\n      restartPolicy: Never\n      nodeSelector:\n        kubernetes.io/hostname: %s\n' "$kubernetes_destination_node" >"$smoke_directory/kubernetes-destination-batchsandbox-template.yaml"
    chmod 0600 "$smoke_directory/kubernetes-destination-opensandbox-kubeconfig.json" "$smoke_directory/kubernetes-destination-opensandbox.toml" "$smoke_directory/kubernetes-destination-batchsandbox-template.yaml"
    kubernetes_destination_opensandbox_upstream_host=$(docker inspect "$kubernetes_destination_node" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
    case "$kubernetes_destination_opensandbox_upstream_host" in
      '' | *' '*) echo "Kubernetes destination node returned an invalid Docker address" >&2; exit 1 ;;
    esac
    docker run -d --name "$kubernetes_destination_opensandbox_container" --label "cloud-agents.dev/test=$project" \
      --network "container:$kubernetes_destination_node" \
      --mount "type=bind,src=$smoke_directory/kubernetes-destination-opensandbox.toml,dst=/etc/opensandbox/config.toml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-destination-batchsandbox-template.yaml,dst=/etc/opensandbox/batchsandbox-template.yaml,readonly" \
      --mount "type=bind,src=$smoke_directory/kubernetes-destination-opensandbox-kubeconfig.json,dst=/run/kube/config,readonly" \
      "$opensandbox_server_image" >/dev/null
    kubernetes_destination_opensandbox_port=8080
    attempt=0
    until curl --noproxy '*' --silent --show-error --fail "http://$kubernetes_destination_opensandbox_upstream_host:$kubernetes_destination_opensandbox_port/health" >/dev/null 2>&1; do
      attempt=$((attempt + 1))
      if [ "$attempt" -ge 180 ]; then
        docker logs "$kubernetes_destination_opensandbox_container" >&2
        exit 1
      fi
      sleep 1
    done
    kubernetes_destination_opensandbox_proxy_port_file="$smoke_directory/kubernetes-destination-opensandbox-proxy.port"
    node "$smoke_directory/opensandbox-proxy.mjs" \
      "$smoke_directory/opensandbox-proxy.crt" "$smoke_directory/opensandbox-proxy.key" \
      "$kubernetes_destination_opensandbox_upstream_host" "$kubernetes_destination_opensandbox_port" "$kubernetes_destination_opensandbox_proxy_port_file" &
    kubernetes_destination_opensandbox_proxy_pid=$!
    attempt=0
    while [ ! -s "$kubernetes_destination_opensandbox_proxy_port_file" ]; do
      attempt=$((attempt + 1))
      if [ "$attempt" -ge 50 ] || ! kill -0 "$kubernetes_destination_opensandbox_proxy_pid" 2>/dev/null; then
        echo "Kubernetes destination OpenSandbox TLS proxy did not start" >&2
        exit 1
      fi
      sleep 0.1
    done
    kubernetes_destination_opensandbox_proxy_port=$(cat "$kubernetes_destination_opensandbox_proxy_port_file")
    printf '%s\n' "$kubernetes_destination_control_plane_token" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.token"
    printf '%s\n' "$kubernetes_ca_data" | openssl base64 -d -A >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.ca.crt"
    printf '{"namespace":"%s","nodeName":"%s"}\n' "$kubernetes_destination_namespace" "$kubernetes_destination_node" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.foundation.json"
    printf '{"namespace":"%s","workerImageRepository":"%s","workerCredentialSecretRef":"cloud-agents-worker-target","workerSpiffeId":"spiffe://cloud-agents.compose/worker-target","workerServerName":"worker-target.example"}\n' \
      "$kubernetes_destination_namespace" "$worker_repository" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.deployment.json"
    printf '{"endpoint":"https://host.docker.internal:%s","apiKey":"%s"}\n' "$kubernetes_destination_opensandbox_proxy_port" "$kubernetes_destination_opensandbox_api_key" \
      >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.opensandbox.json"
    chmod 0444 "$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.ca.crt" \
      "$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.token" \
      "$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.foundation.json" \
      "$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.deployment.json" \
      "$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.opensandbox.json"
  fi

  kubernetes_ctl -n "$kubernetes_runtime_namespace" run worker-image-probe --restart=Never \
    --image="$worker_repository@$worker_release_digest" --image-pull-policy=Never --command -- /bin/true >/dev/null
  kubernetes_ctl -n "$kubernetes_runtime_namespace" wait pod/worker-image-probe --for=jsonpath='{.status.phase}'=Succeeded --timeout=60s >/dev/null
  kubernetes_ctl -n "$kubernetes_runtime_namespace" delete pod worker-image-probe --wait=true --timeout=60s >/dev/null
fi

CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY="$worker_repository" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
  node <<'NODE'
const { chmodSync, writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const workerImageRepository = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY;
const workerCredentialRef = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF;
if (![state, workerImageRepository, workerCredentialRef].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Docker target deployment inputs");
}
const descriptor = {
  workerImageRepository,
  workerCredentialRef,
  workerSpiffeId: "spiffe://cloud-agents.compose/worker-target",
  workerServerName: "host.docker.internal",
};
const path = `${state}/docker-target-credentials/docker-compose-target/deployment.json`;
writeFileSync(path, `${JSON.stringify(descriptor)}\n`);
chmodSync(path, 0o444);
const kubernetesDescriptorPath = `${state}/kubernetes-target-credentials/kubernetes-compose-target.deployment.json`;
writeFileSync(kubernetesDescriptorPath, `${JSON.stringify({ namespace: "cloud-agents-target", workerImageRepository, workerCredentialSecretRef: "cloud-agents-worker-target", workerSpiffeId: "spiffe://cloud-agents.compose/worker-target", workerServerName: "worker-target.example" })}\n`);
chmodSync(kubernetesDescriptorPath, 0o444);
NODE

cp "$smoke_directory/ca.crt" "$smoke_directory/fake-kubectl-state/ca.crt"
: >"$smoke_directory/fake-kubeconfig"
chmod 0600 "$smoke_directory/fake-kubeconfig"
cat >"$smoke_directory/fake-kubectl" <<'SH'
#!/bin/sh
set -eu
state=${CLOUD_AGENTS_FAKE_KUBECTL_STATE:?}
printf '%s\n' "$*" >>"$state/calls"
case "$*" in
  *' config view '*'certificate-authority-data'*) openssl base64 -A -in "$state/ca.crt" ;;
  *' config view '*'cluster.server'*) printf '%s' 'https://kubernetes-target.example:6443' ;;
  *' apply '*) cat >"$state/rbac.yaml" ;;
  *' auth can-i '*) printf '%s\n' yes ;;
  *' get secret '*) ;;
  *' create token '*) printf '%s\n' fake-service-account-token ;;
  *' create secret generic '*) ;;
  *) echo "unexpected fake kubectl command: $*" >&2; exit 1 ;;
esac
SH
chmod 0755 "$smoke_directory/fake-kubectl"

prepare_kubernetes_target() {
  CLOUD_AGENTS_FAKE_KUBECTL_STATE="$smoke_directory/fake-kubectl-state" \
  CLOUD_AGENTS_KUBECONFIG="$smoke_directory/fake-kubeconfig" \
  CLOUD_AGENTS_KUBERNETES_CONTEXT=target-context \
  CLOUD_AGENTS_KUBERNETES_NAMESPACE=cloud-agents-target \
  CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT=control-plane \
  CLOUD_AGENTS_KUBERNETES_TOKEN_DURATION=24h \
  CLOUD_AGENTS_TARGET_CREDENTIAL_REF=kubernetes-prepared-target \
  CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR="$smoke_directory/prepared-kubernetes-target-credentials" \
  CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY="$worker_repository" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF=cloud-agents-worker-target \
  CLOUD_AGENTS_WORKER_CREDENTIAL_DIR="$smoke_directory/target-worker-credentials" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF=cloud-agents-provider-target \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR="$smoke_directory/target-provider-credentials" \
  CLOUD_AGENTS_TENANT=tenant-compose-smoke \
  CLOUD_AGENTS_WORKER_SPIFFE_ID=spiffe://cloud-agents.compose/worker-target \
  CLOUD_AGENTS_WORKER_SERVER_NAME=worker-target.example \
  KUBECTL="$smoke_directory/fake-kubectl" \
    sh "$smoke_directory/deployment/scripts/prepare-platform-kubernetes-target.sh"
}
kubernetes_prepare_output=$(prepare_kubernetes_target)
case "$kubernetes_prepare_output" in
  *'endpoint=https://kubernetes-target.example:6443 credentialRef=kubernetes-prepared-target'*) ;;
  *) echo "Kubernetes target preparation returned an invalid result" >&2; exit 1 ;;
esac
if prepare_kubernetes_target >/dev/null 2>&1; then
  echo "Kubernetes target preparation overwrote existing credentials" >&2
  exit 1
fi
CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" node <<'NODE'
const { readFileSync, statSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const directory = `${state}/prepared-kubernetes-target-credentials`;
const files = ["kubernetes-prepared-target.ca.crt", "kubernetes-prepared-target.token", "kubernetes-prepared-target.deployment.json"];
for (const file of files) {
  if ((statSync(`${directory}/${file}`).mode & 0o777) !== 0o400) throw new Error(`${file} mode is not 0400`);
}
if (!readFileSync(`${directory}/kubernetes-prepared-target.ca.crt`).equals(readFileSync(`${state}/ca.crt`))) throw new Error("prepared Kubernetes CA changed");
if (readFileSync(`${directory}/kubernetes-prepared-target.token`, "utf8") !== "fake-service-account-token\n") throw new Error("prepared Kubernetes token changed");
const descriptor = JSON.parse(readFileSync(`${directory}/kubernetes-prepared-target.deployment.json`, "utf8"));
if (descriptor.namespace !== "cloud-agents-target" || descriptor.workerCredentialSecretRef !== "cloud-agents-worker-target") throw new Error("prepared Kubernetes descriptor changed");
const rbac = readFileSync(`${state}/fake-kubectl-state/rbac.yaml`, "utf8");
if (!rbac.includes('resourceNames: ["cloud-agents-worker-target", "cloud-agents-provider-target"]') || !rbac.includes('resources: ["pods/exec"]') || !rbac.includes('verbs: ["get", "create"]') || !rbac.includes('verbs: ["get", "list", "create", "patch", "delete"]')) throw new Error("prepared Kubernetes RBAC changed");
if (readFileSync(`${state}/fake-kubectl-state/calls`, "utf8").includes("fake-service-account-token")) throw new Error("Kubernetes token entered command log");
NODE

prepare_target_credentials() {
  CLOUD_AGENTS_WORKER_IMAGE="$worker_reference" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_DIR="$smoke_directory/target-worker-credentials" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF="$target_provider_credentials_volume" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR="$smoke_directory/target-provider-credentials" \
  CLOUD_AGENTS_TENANT=tenant-compose-smoke \
    sh "$smoke_directory/deployment/scripts/prepare-platform-docker-target.sh"
}
prepare_target_credentials >/dev/null
if prepare_target_credentials >/dev/null 2>&1; then
  echo "Docker target preparation overwrote non-empty credential volumes" >&2
  exit 1
fi
if [ -n "$real_provider_credentials_directory" ]; then
  cp "$real_provider_credentials_directory/tenant-compose-smoke.codex.json" \
    "$real_provider_credentials_directory/tenant-compose-smoke.claudeAgent.json" \
    "$real_provider_credentials_directory/tenant-compose-smoke.pi.json" \
    "$real_provider_credentials_directory/tenant-compose-smoke.deepseek-harness.json" \
    "$smoke_directory/provider-credentials/"
  chmod 0444 "$smoke_directory/provider-credentials/tenant-compose-smoke.codex.json" \
    "$smoke_directory/provider-credentials/tenant-compose-smoke.claudeAgent.json" \
    "$smoke_directory/provider-credentials/tenant-compose-smoke.pi.json" \
    "$smoke_directory/provider-credentials/tenant-compose-smoke.deepseek-harness.json"
  docker run --rm --user 0 --entrypoint /bin/sh \
    -v "$target_provider_credentials_volume:/target" \
    -v "$real_provider_credentials_directory:/source:ro" \
    postgres:17.6-bookworm -ec \
    'cp /source/tenant-compose-smoke.codex.json /source/tenant-compose-smoke.claudeAgent.json /source/tenant-compose-smoke.pi.json /source/tenant-compose-smoke.deepseek-harness.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
fi

start_cross_node_destination() {
  destination_dind_image='docker@sha256:5efed980cba3fc126cf54e21a5a6ff8849d05b6e0623d6e7612f48e9cd6cd17e'
  destination_execd_image='sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd'
  destination_egress_image='sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a'
  destination_api_key_file="$smoke_directory/destination-opensandbox-api-key"
  printf '%s' "$(openssl rand -hex 32)" >"$destination_api_key_file"
  chmod 0600 "$destination_api_key_file"
  docker run -d --privileged --name "$destination_node_container" \
    --label "cloud-agents.dev/test=$project" -e DOCKER_TLS_CERTDIR= \
    --add-host host.docker.internal:host-gateway \
    -p 127.0.0.1::2375 -p 127.0.0.1::8080 \
    --mount "type=bind,src=$smoke_directory,dst=/dind-config" \
    "$destination_dind_image" --host=tcp://0.0.0.0:2375 --tls=false >/dev/null
  destination_docker_port=$(docker port "$destination_node_container" 2375/tcp | sed 's/.*://')
  destination_opensandbox_port=$(docker port "$destination_node_container" 8080/tcp | sed 's/.*://')
  attempt=0
  until docker -H "tcp://127.0.0.1:$destination_docker_port" info >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "destination Docker node did not start" >&2
      exit 1
    fi
    sleep 0.25
  done
  destination_inner_gateway=$(docker -H "tcp://127.0.0.1:$destination_docker_port" \
    network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')
  destination_api_key=$(cat "$destination_api_key_file")
  cat >"$smoke_directory/destination-opensandbox.toml" <<EOF
[server]
host="0.0.0.0"
eip="host.docker.internal"
port=8080
api_key="$destination_api_key"
[runtime]
type="docker"
execd_image="$destination_execd_image"
[docker]
network_mode="bridge"
host_ip="$destination_inner_gateway"
port_range_min=49530
port_range_max=49740
[egress]
image="$destination_egress_image"
mode="dns+nft"
[storage]
allowed_host_paths=[]
[store]
type="sqlite"
path="/tmp/opensandbox.db"
EOF
  chmod 0600 "$smoke_directory/destination-opensandbox.toml"
  docker tag "$opensandbox_server_image" "${project}-opensandbox-server:fixed"
  docker image save "${project}-opensandbox-server:fixed" \
    -o "$smoke_directory/destination-images.tar"
  docker -H "tcp://127.0.0.1:$destination_docker_port" image load \
    -i "$smoke_directory/destination-images.tar" >/dev/null
  docker -H "tcp://127.0.0.1:$destination_docker_port" pull "$destination_execd_image" >/dev/null
  docker -H "tcp://127.0.0.1:$destination_docker_port" pull "$destination_egress_image" >/dev/null
  docker exec -d "$destination_node_container" sh -c \
    "exec nc -lk -p '$registry_port' -e nc host.docker.internal '$registry_port'"
  docker -H "tcp://127.0.0.1:$destination_docker_port" pull "$worker_reference" >/dev/null
  docker -H "tcp://127.0.0.1:$destination_docker_port" run -d --name opensandbox \
    --add-host host.docker.internal:host-gateway -p 8080:8080 \
    --mount type=bind,src=/dind-config/destination-opensandbox.toml,dst=/etc/opensandbox/config.toml,readonly \
    --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
    "${project}-opensandbox-server:fixed" >/dev/null
  attempt=0
  until curl --silent --show-error --fail "http://127.0.0.1:$destination_opensandbox_port/health" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      docker -H "tcp://127.0.0.1:$destination_docker_port" logs opensandbox >&2
      exit 1
    fi
    sleep 1
  done
  destination_docker_proxy_port_file="$smoke_directory/destination-docker-proxy.port"
  node "$smoke_directory/docker-proxy.mjs" "tcp://127.0.0.1:$destination_docker_port" \
    "$smoke_directory/docker-target-ca.crt" "$smoke_directory/docker-target-server.crt" \
    "$smoke_directory/docker-target-server.key" "$destination_docker_proxy_port_file" &
  destination_docker_proxy_pid=$!
  destination_opensandbox_proxy_port_file="$smoke_directory/destination-opensandbox-proxy.port"
  node "$smoke_directory/opensandbox-proxy.mjs" \
    "$smoke_directory/opensandbox-proxy.crt" "$smoke_directory/opensandbox-proxy.key" \
    "$destination_opensandbox_port" "$destination_opensandbox_proxy_port_file" &
  destination_opensandbox_proxy_pid=$!
  attempt=0
  while [ ! -s "$destination_docker_proxy_port_file" ] || [ ! -s "$destination_opensandbox_proxy_port_file" ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 50 ]; then
      echo "destination target proxies did not start" >&2
      exit 1
    fi
    sleep 0.1
  done
  destination_docker_proxy_port=$(cat "$destination_docker_proxy_port_file")
  destination_opensandbox_proxy_port=$(cat "$destination_opensandbox_proxy_port_file")
  CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_PORT="$destination_opensandbox_proxy_port" \
  CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_KEY_FILE="$destination_api_key_file" \
  CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL="$smoke_directory/docker-target-credentials/docker-compose-target-restore/opensandbox.json" \
    node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
writeFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL,
  `${JSON.stringify({ endpoint: `https://host.docker.internal:${process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_PORT}`, apiKey: readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_KEY_FILE, "utf8") })}\n`,
  { mode: 0o444 });
chmodSync(process.env.CLOUD_AGENTS_COMPOSE_OPEN_SANDBOX_CREDENTIAL, 0o444);
NODE
  cp "$smoke_directory/docker-target-credentials/docker-compose-target/deployment.json" \
    "$smoke_directory/docker-target-credentials/docker-compose-target-restore/deployment.json"
  chmod 0444 "$smoke_directory/docker-target-credentials/docker-compose-target-restore/deployment.json"
}

if [ "$cross_node_recovery" -eq 1 ] && { [ "$cross_node_environment" = docker ] || [ "$remote_runtime" -eq 1 ]; }; then
  start_cross_node_destination
fi

wait_ready
organization_output=$(cloud_agentsctl_user --request-id compose-smoke-organizations organization list)
case "$organization_output" in
  *'"uid":"organization-compose-smoke"'*) ;;
  *) echo "Compose bootstrap organization is unavailable" >&2; exit 1 ;;
esac
project_output=$(cloud_agentsctl_user --request-id compose-smoke-project-create \
  --idempotency-key compose-smoke-project-create project create --name compose-smoke-project \
  --display-name "Compose Smoke Project" --organization-id organization-compose-smoke)
project_id=$(printf '%s' "$project_output" | node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.metadata.uid)')
case "$project_id" in project-*) ;; *) echo "Compose project id is invalid" >&2; exit 1 ;; esac

remote_target_id=
if [ "$remote_runtime" -eq 1 ]; then
  remote_enrollment_id=remote-worker-compose-runtime
  remote_worker_credential_ref=remote-runtime
  remote_worker_bootstrap_docker_proxy_port=$destination_docker_proxy_port
  remote_worker_bootstrap_opensandbox_proxy_port=$destination_opensandbox_proxy_port
  remote_worker_bootstrap_api_key_file=$destination_api_key_file
  remote_worker_incarnation=incarnation-compose-runtime
  remote_worker_credential_source_directory="$smoke_directory/docker-target-credentials/docker-compose-target-restore"
  remote_worker_state_directory="$smoke_directory/remote-worker-node"
  if [ "$cross_node_environment" = remote-worker ]; then
    remote_enrollment_id=remote-worker-compose-source
    remote_worker_credential_ref=remote-runtime-source
    remote_worker_bootstrap_docker_proxy_port=$docker_proxy_port
    remote_worker_bootstrap_opensandbox_proxy_port=$opensandbox_proxy_port
    remote_worker_bootstrap_api_key_file="$smoke_directory/opensandbox-api-key"
    remote_worker_incarnation=incarnation-compose-runtime-source
    remote_worker_credential_source_directory="$smoke_directory/docker-target-credentials/docker-compose-target"
  fi
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/remote-worker-enrollments" \
    compose-remote-runtime-enrollment --header "Idempotency-Key: compose-remote-runtime-enrollment-$remote_enrollment_id" \
    --data "{\"enrollmentId\":\"$remote_enrollment_id\",\"workerId\":\"worker-compose-runtime-$remote_enrollment_id\",\"workerName\":\"worker-compose-runtime-$remote_enrollment_id\",\"ttlSeconds\":900}" \
    >"$smoke_directory/remote-worker-enrollment.json"
  mkdir -p "$remote_worker_state_directory/credentials/$remote_worker_credential_ref"
  chmod 0700 "$remote_worker_state_directory/credentials" "$remote_worker_state_directory/credentials/$remote_worker_credential_ref"
  cp "$remote_worker_credential_source_directory/ca.pem" \
    "$remote_worker_credential_source_directory/cert.pem" \
    "$remote_worker_credential_source_directory/key.pem" \
    "$remote_worker_state_directory/credentials/$remote_worker_credential_ref/"
  chmod 0400 "$remote_worker_state_directory/credentials/$remote_worker_credential_ref/"*.pem
  CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL="$remote_worker_state_directory/credentials/$remote_worker_credential_ref/opensandbox.json" \
  CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_PORT="$remote_worker_bootstrap_opensandbox_proxy_port" \
  CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_KEY_FILE="$remote_worker_bootstrap_api_key_file" node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
writeFileSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL, `${JSON.stringify({
  endpoint: `https://host.docker.internal:${process.env.CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_PORT}`,
  apiKey: readFileSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_KEY_FILE, "utf8"),
})}\n`);
chmodSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL, 0o400);
NODE
  docker run --rm --platform "$image_platform" \
    --label "cloud-agents.dev/test=$project" --network "${project}_default" --add-host host.docker.internal:host-gateway \
    --volume "$candidate_directory:/release:ro" --volume "$smoke_directory:/smoke:ro" \
    --volume "$remote_worker_state_directory:/node-output" \
    --env CLOUD_AGENTS_PLATFORM_RELEASE_DIR=/release \
    --env CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR=/node-output/install \
    --env CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL=https://control-plane:8080 \
    --env CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE=/smoke/ca.crt \
    --env CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE=/smoke/remote-worker-bootstrap-token \
    --env CLOUD_AGENTS_REMOTE_WORKER_TENANT=tenant-compose-smoke \
    --env "CLOUD_AGENTS_REMOTE_WORKER_PROJECT=$project_id" \
    --env "CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT=$remote_enrollment_id" \
    --env "CLOUD_AGENTS_REMOTE_WORKER_INCARNATION=$remote_worker_incarnation" \
    --env CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES=docker,exec,files,network-dns-nft,preview,pty,workspace-snapshot,workspace-volume \
    --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS=4000 \
    --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES=8589934592 \
    --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES=68719476736 \
    --env "CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT=https://host.docker.internal:$remote_worker_bootstrap_docker_proxy_port" \
    --env CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY=/node-output/credentials \
    --env "CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF=$remote_worker_credential_ref" \
    debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 \
    sh /smoke/deployment/scripts/bootstrap-platform-remote-worker.sh >/dev/null
  docker run -d --platform "$image_platform" --name "$remote_worker_container" \
    --label "cloud-agents.dev/test=$project" --network "${project}_default" --add-host host.docker.internal:host-gateway \
    --volume "$remote_worker_state_directory:/node-output" \
    --env SSL_CERT_FILE=/node-output/install/control-plane-ca.pem \
    debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 \
    /node-output/install/run.sh >/dev/null
  remote_target_id=$(CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" CLOUD_AGENTS_COMPOSE_REMOTE_ENROLLMENT="$remote_enrollment_id" node -e \
    'const {createHash}=require("node:crypto");process.stdout.write("rwt-"+createHash("sha256").update(`tenant-compose-smoke|${process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID}|${process.env.CLOUD_AGENTS_COMPOSE_REMOTE_ENROLLMENT}`).digest("hex"))')
  attempt=0
  while :; do
    if remote_target_output=$(cloud_agentsctl --project "$project_id" --target "$remote_target_id" \
      --request-id compose-remote-runtime-target-get target get 2>/dev/null); then
      case "$remote_target_output" in
        *'"targetKind":"remote-worker"'*'"observedPhase":"ready"'*) break ;;
      esac
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "Compose outbound RemoteWorker target did not become ready" >&2
      docker logs "$remote_worker_container" >&2 || true
      exit 1
    fi
    sleep 1
  done
  remote_target_restore_id=
  if [ "$cross_node_environment" = remote-worker ]; then
    remote_destination_enrollment_id=remote-worker-compose-destination
    remote_target_restore_id=$(CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" CLOUD_AGENTS_COMPOSE_REMOTE_ENROLLMENT="$remote_destination_enrollment_id" node -e \
      'const {createHash}=require("node:crypto");process.stdout.write("rwt-"+createHash("sha256").update(`tenant-compose-smoke|${process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID}|${process.env.CLOUD_AGENTS_COMPOSE_REMOTE_ENROLLMENT}`).digest("hex"))')
    control_plane_api "$smoke_directory/admin-curl.conf" POST \
      "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/remote-worker-enrollments" \
      compose-remote-runtime-destination-enrollment --header "Idempotency-Key: compose-remote-runtime-destination-enrollment" \
      --data "{\"enrollmentId\":\"$remote_destination_enrollment_id\",\"workerId\":\"worker-compose-runtime-destination\",\"workerName\":\"worker-compose-runtime-destination\",\"ttlSeconds\":900}" \
      >"$smoke_directory/remote-worker-destination-enrollment.json"
    remote_destination_state_directory="$smoke_directory/remote-worker-node-restore"
    remote_destination_credential_ref=remote-runtime-destination
    mkdir -p "$remote_destination_state_directory/credentials/$remote_destination_credential_ref"
    chmod 0700 "$remote_destination_state_directory/credentials" "$remote_destination_state_directory/credentials/$remote_destination_credential_ref"
    cp "$smoke_directory/docker-target-credentials/docker-compose-target-restore/ca.pem" \
      "$smoke_directory/docker-target-credentials/docker-compose-target-restore/cert.pem" \
      "$smoke_directory/docker-target-credentials/docker-compose-target-restore/key.pem" \
      "$remote_destination_state_directory/credentials/$remote_destination_credential_ref/"
    chmod 0400 "$remote_destination_state_directory/credentials/$remote_destination_credential_ref/"*.pem
    CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL="$remote_destination_state_directory/credentials/$remote_destination_credential_ref/opensandbox.json" \
    CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_PORT="$destination_opensandbox_proxy_port" \
    CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_KEY_FILE="$destination_api_key_file" node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
writeFileSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL, `${JSON.stringify({
  endpoint: `https://host.docker.internal:${process.env.CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_PORT}`,
  apiKey: readFileSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_OPEN_SANDBOX_KEY_FILE, "utf8"),
})}\n`);
chmodSync(process.env.CLOUD_AGENTS_COMPOSE_REMOTE_CREDENTIAL, 0o400);
NODE
    docker run --rm --platform "$image_platform" \
      --label "cloud-agents.dev/test=$project" --network "${project}_default" --add-host host.docker.internal:host-gateway \
      --volume "$candidate_directory:/release:ro" --volume "$smoke_directory:/smoke:ro" \
      --volume "$remote_destination_state_directory:/node-output" \
      --env CLOUD_AGENTS_PLATFORM_RELEASE_DIR=/release \
      --env CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR=/node-output/install \
      --env CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL=https://control-plane:8080 \
      --env CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE=/smoke/ca.crt \
      --env CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE=/smoke/remote-worker-bootstrap-token \
      --env CLOUD_AGENTS_REMOTE_WORKER_TENANT=tenant-compose-smoke \
      --env "CLOUD_AGENTS_REMOTE_WORKER_PROJECT=$project_id" \
      --env "CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT=$remote_destination_enrollment_id" \
      --env CLOUD_AGENTS_REMOTE_WORKER_INCARNATION=incarnation-compose-runtime-destination \
      --env CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES=docker,exec,files,network-dns-nft,preview,pty,workspace-snapshot,workspace-volume \
      --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS=4000 \
      --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES=8589934592 \
      --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES=68719476736 \
      --env "CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT=https://host.docker.internal:$destination_docker_proxy_port" \
      --env CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY=/node-output/credentials \
      --env "CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF=$remote_destination_credential_ref" \
      debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 \
      sh /smoke/deployment/scripts/bootstrap-platform-remote-worker.sh >/dev/null
    docker run -d --platform "$image_platform" --name "$remote_worker_destination_container" \
      --label "cloud-agents.dev/test=$project" --network "${project}_default" --add-host host.docker.internal:host-gateway \
      --volume "$remote_destination_state_directory:/node-output" \
      --env SSL_CERT_FILE=/node-output/install/control-plane-ca.pem \
      debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 \
      /node-output/install/run.sh >/dev/null
    attempt=0
    while :; do
      if remote_target_restore_output=$(cloud_agentsctl --project "$project_id" --target "$remote_target_restore_id" \
        --request-id compose-remote-runtime-destination-target-get target get 2>/dev/null); then
        case "$remote_target_restore_output" in
          *'"targetKind":"remote-worker"'*'"observedPhase":"ready"'*) break ;;
        esac
      fi
      attempt=$((attempt + 1))
      if [ "$attempt" -ge 60 ]; then
        echo "Compose destination RemoteWorker target did not become ready" >&2
        docker logs "$remote_worker_destination_container" >&2 || true
        exit 1
      fi
      sleep 1
    done
  fi
fi

kubernetes_target_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-register --idempotency-key compose-smoke-kubernetes-target-register \
  target register --target-name kubernetes-compose-target --kind kubernetes \
  --target-endpoint "https://host.docker.internal:$kubernetes_api_port" \
  --credential-ref kubernetes-compose-target)
case "$kubernetes_target_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"unprobed"'*) ;;
  *) echo "Compose Kubernetes target was not registered" >&2; exit 1 ;;
esac
kubernetes_probe_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-probe --idempotency-key compose-smoke-kubernetes-target-probe \
  target probe --expected-generation 1)
case "$kubernetes_probe_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":"1.34"'*'"engineVersion":"v1.34.2"'*'"os":"linux"'*'"architecture":"arm64"'*) ;;
  *) echo "Compose Kubernetes target probe did not become ready" >&2; exit 1 ;;
esac
kubernetes_target_get_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-get target get)
case "$kubernetes_target_get_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Kubernetes target ready state was not persisted" >&2; exit 1 ;;
esac
kubernetes_cleanup_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-cleanup --idempotency-key compose-smoke-kubernetes-target-cleanup \
  target cleanup --expected-generation 1 --confirm-target-id kubernetes-compose-target)
case "$kubernetes_cleanup_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Kubernetes target cleanup failed" >&2; exit 1 ;;
esac

if [ "$kubernetes_runtime" -eq 1 ]; then
  kubernetes_runtime_target_output=$(cloud_agentsctl --project "$project_id" --target "$kubernetes_runtime_target_id" \
    --request-id compose-kubernetes-runtime-target-register --idempotency-key compose-kubernetes-runtime-target-register \
    target register --target-name "$kubernetes_runtime_target_id" --kind kubernetes \
    --target-endpoint "$kubernetes_target_endpoint" --credential-ref "$kubernetes_runtime_credential_ref")
  case "$kubernetes_runtime_target_output" in
    *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"unprobed"'*) ;;
    *) echo "Compose real Kubernetes Runtime target was not registered" >&2; exit 1 ;;
  esac
  kubernetes_runtime_probe_output=$(cloud_agentsctl --project "$project_id" --target "$kubernetes_runtime_target_id" \
    --request-id compose-kubernetes-runtime-target-probe --idempotency-key compose-kubernetes-runtime-target-probe \
    target probe --expected-generation 1)
  case "$kubernetes_runtime_probe_output" in
    *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":'*'"engineVersion":'*) ;;
    *) echo "Compose real Kubernetes Runtime target did not become ready" >&2; exit 1 ;;
  esac
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_destination_target_output=$(cloud_agentsctl --project "$project_id" --target "$kubernetes_destination_target_id" \
      --request-id compose-kubernetes-restore-target-register --idempotency-key compose-kubernetes-restore-target-register \
      target register --target-name "$kubernetes_destination_target_id" --kind kubernetes \
      --target-endpoint "$kubernetes_target_endpoint" --credential-ref "$kubernetes_destination_credential_ref")
    case "$kubernetes_destination_target_output" in
      *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"unprobed"'*) ;;
      *) echo "Compose real Kubernetes restore target was not registered" >&2; exit 1 ;;
    esac
    kubernetes_destination_probe_output=$(cloud_agentsctl --project "$project_id" --target "$kubernetes_destination_target_id" \
      --request-id compose-kubernetes-restore-target-probe --idempotency-key compose-kubernetes-restore-target-probe \
      target probe --expected-generation 1)
    case "$kubernetes_destination_probe_output" in
      *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":'*'"engineVersion":'*) ;;
      *) echo "Compose real Kubernetes restore target did not become ready" >&2; exit 1 ;;
    esac
  fi
fi

target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-register --idempotency-key compose-smoke-target-register \
  target register --target-name docker-compose-target --kind docker \
  --target-endpoint "https://host.docker.internal:$docker_proxy_port" \
  --credential-ref docker-compose-target)
case "$target_output" in
  *'"generation":1'*'"targetKind":"docker"'*'"observedPhase":"unprobed"'*) ;;
  *) echo "Compose Docker target was not registered" >&2; exit 1 ;;
esac
probe_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-probe --idempotency-key compose-smoke-target-probe \
  target probe --expected-generation 1)
case "$probe_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":'*'"engineVersion":'*) ;;
  *) echo "Compose Docker target probe did not become ready" >&2; exit 1 ;;
esac
target_get_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-get target get)
case "$target_get_output" in
  *'"generation":1'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Docker target ready state was not persisted" >&2; exit 1 ;;
esac
if [ "$cross_node_recovery" -eq 1 ] && { [ "$cross_node_environment" = docker ] || [ "$remote_runtime" -eq 1 ]; }; then
  destination_target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target-restore \
    --request-id compose-smoke-restore-target-register --idempotency-key compose-smoke-restore-target-register \
    target register --target-name docker-compose-target-restore --kind docker \
    --target-endpoint "https://host.docker.internal:$destination_docker_proxy_port" \
    --credential-ref docker-compose-target-restore)
  case "$destination_target_output" in
    *'"generation":1'*'"targetKind":"docker"'*'"observedPhase":"unprobed"'*) ;;
    *) echo "Compose restore Docker target was not registered" >&2; exit 1 ;;
  esac
  destination_probe_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target-restore \
    --request-id compose-smoke-restore-target-probe --idempotency-key compose-smoke-restore-target-probe \
    target probe --expected-generation 1)
  case "$destination_probe_output" in
    *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":'*'"engineVersion":'*) ;;
    *) echo "Compose restore Docker target did not become ready" >&2; exit 1 ;;
  esac
fi

legacy_target_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-legacy-target-removed" --output /dev/null --write-out '%{http_code}' \
  "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/deployment-targets")
legacy_lease_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-legacy-lease-removed" --output /dev/null --write-out '%{http_code}' \
  "https://$endpoint/v1/managed-host/tenants/tenant-compose-smoke/projects/$project_id/environment-leases")
if [ "$legacy_target_status" -ne 404 ] || [ "$legacy_lease_status" -ne 404 ]; then
  echo "Compose still exposed legacy User Target/Lease routes: target=$legacy_target_status lease=$legacy_lease_status" >&2
  exit 1
fi

release_id=compose-worker-release
release_create_file="$smoke_directory/release-create.json"
release_create_body=$(printf '{"releaseId":"%s","releaseName":"%s","imageRepository":"%s","releaseDigest":"%s","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["%s"],"verificationEvidenceDigest":"%s"}' \
  "$release_id" "$release_id" "$worker_repository" "$worker_release_digest" "$image_platform" "$worker_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-release-create --header "Idempotency-Key: compose-smoke-release-create" \
  --data "$release_create_body" >"$release_create_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$release_create_file" \
CLOUD_AGENTS_COMPOSE_RELEASE_ID="$release_id" \
CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.kind !== "WorkerRelease" || value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID ||
    value.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST ||
    value.spec?.status !== "approved" || value.spec?.verificationState !== "attested" ||
    value.metadata?.resourceVersion !== "1") {
  throw new Error("Admin API did not persist the approved Worker release");
}
for (const forbidden of ["credentialRef", "providerCredentialRef", "endpoint", "secret", "prompt", "artifact"]) {
  if (JSON.stringify(value).toLowerCase().includes(forbidden.toLowerCase())) {
    throw new Error(`Worker release response exposed ${forbidden}`);
  }
}
NODE
release_replay_file="$smoke_directory/release-replay.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-release-create --header "Idempotency-Key: compose-smoke-release-create" \
  --data "$release_create_body" >"$release_replay_file"
if ! cmp -s "$release_create_file" "$release_replay_file"; then
  echo "Compose Worker release idempotent replay drifted" >&2
  exit 1
fi
release_list_file="$smoke_directory/release-list.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases?pageSize=200" \
  compose-smoke-release-list >"$release_list_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$release_list_file" CLOUD_AGENTS_COMPOSE_RELEASE_ID="$release_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.kind !== "WorkerReleasePage" || value.workerReleases?.length !== 1 ||
    value.workerReleases[0]?.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID) {
  throw new Error("Admin API Worker release catalog drifted");
}
NODE
user_admin_release_file="$smoke_directory/user-admin-release-denied.json"
user_admin_release_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-release-denied" \
  --output "$user_admin_release_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases?pageSize=200")
if [ "$user_admin_release_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_release_file"; then
  echo "Compose ordinary User token was not denied by the Worker Release Admin API" >&2
  exit 1
fi
upgrade_release_id=compose-worker-release-upgrade
upgrade_release_create_file="$smoke_directory/upgrade-release-create.json"
upgrade_release_create_body=$(printf '{"releaseId":"%s","releaseName":"%s","imageRepository":"%s","releaseDigest":"%s","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["%s"],"verificationEvidenceDigest":"%s"}' \
  "$upgrade_release_id" "$upgrade_release_id" "$worker_repository" "$worker_upgrade_release_digest" "$image_platform" "$worker_upgrade_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-upgrade-release-create --header "Idempotency-Key: compose-smoke-upgrade-release-create" \
  --data "$upgrade_release_create_body" >"$upgrade_release_create_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$upgrade_release_create_file" \
  CLOUD_AGENTS_COMPOSE_RELEASE_ID="$upgrade_release_id" \
  CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST="$worker_upgrade_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID ||
    value.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST ||
    value.spec?.status !== "approved" || value.spec?.verificationState !== "attested") {
  throw new Error("Admin API did not persist the approved upgrade Worker release");
}
NODE
storage_policy_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/storage-policies/storage-compose"
storage_policy_file="$smoke_directory/storage-policy.json"
storage_policy_body='{"expectedResourceVersion":"0","policyName":"storage-compose","userSummary":"20 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":21474836480,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"snapshotBackendRef":"snapshot-compose","artifactBackendRef":"artifact-compose","allowWorkspaceReuse":true}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$storage_policy_path" \
  compose-smoke-storage-policy --header "Idempotency-Key: compose-smoke-storage-policy" \
  --data "$storage_policy_body" >"$storage_policy_file"
CLOUD_AGENTS_COMPOSE_STORAGE_FILE="$storage_policy_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_STORAGE_FILE, "utf8"));
if (value.kind !== "StoragePolicy" || value.metadata?.uid !== "storage-compose" ||
    value.metadata?.resourceVersion !== "1" || value.spec?.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.spec?.workspaceType !== "managed-volume" || value.spec?.workspaceCapacityBytes !== 21474836480 ||
    value.spec?.retentionSeconds !== 0 || value.spec?.cleanupOnLeaseTermination !== true ||
    value.spec?.allowWorkspaceReuse !== true || value.spec?.userSummary !== "20 GiB managed workspace") {
  throw new Error("Admin API did not persist the Storage Policy authority");
}
NODE
storage_policy_list_file="$smoke_directory/storage-policy-list.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/storage-policies?pageSize=200" \
  compose-smoke-storage-policy-list >"$storage_policy_list_file"
CLOUD_AGENTS_COMPOSE_STORAGE_FILE="$storage_policy_list_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_STORAGE_FILE, "utf8"));
if (value.kind !== "StoragePolicyPage" || value.storagePolicies?.length !== 1 ||
    value.storagePolicies[0]?.metadata?.uid !== "storage-compose") {
  throw new Error("Admin API Storage Policy catalog drifted");
}
NODE
storage_policy_audit_file="$smoke_directory/storage-policy-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET "$storage_policy_path/audit-events?pageSize=200" \
  compose-smoke-storage-policy-audit >"$storage_policy_audit_file"
CLOUD_AGENTS_COMPOSE_STORAGE_FILE="$storage_policy_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_STORAGE_FILE, "utf8"));
if (value.kind !== "AdminAuditEventPage" || value.events?.length !== 1 ||
    value.events[0]?.action !== "storage-policy.set" || value.events[0]?.resourceKind !== "StoragePolicy") {
  throw new Error("Admin API Storage Policy audit drifted");
}
NODE
user_admin_storage_file="$smoke_directory/user-admin-storage-denied.json"
user_admin_storage_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-storage-denied" \
  --output "$user_admin_storage_file" --write-out '%{http_code}' \
  "https://$endpoint$storage_policy_path")
if [ "$user_admin_storage_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_storage_file"; then
  echo "Compose ordinary User token was not denied by the Storage Policy Admin API" >&2
  exit 1
fi
network_policy_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/network-policies/network-compose"
network_policy_file="$smoke_directory/network-policy.json"
network_policy_body='{"expectedResourceVersion":"0","policyName":"network-compose","userSummary":"Public internet access","defaultEgress":"public","allowedEgress":[],"ingressEnabled":false,"previewEnabled":false}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$network_policy_path" \
  compose-smoke-network-policy --header "Idempotency-Key: compose-smoke-network-policy" \
  --data "$network_policy_body" >"$network_policy_file"
CLOUD_AGENTS_COMPOSE_NETWORK_FILE="$network_policy_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_NETWORK_FILE, "utf8"));
if (value.kind !== "NetworkPolicy" || value.metadata?.uid !== "network-compose" ||
    value.metadata?.resourceVersion !== "1" || value.spec?.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.spec?.defaultEgress !== "public" || value.spec?.ingressEnabled !== false ||
    value.spec?.previewEnabled !== false || value.spec?.userSummary !== "Public internet access") {
  throw new Error("Admin API did not persist the Network Policy authority");
}
NODE
network_policy_replay_file="$smoke_directory/network-policy-replay.json"
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$network_policy_path" \
  compose-smoke-network-policy-replay --header "Idempotency-Key: compose-smoke-network-policy" \
  --data "$network_policy_body" >"$network_policy_replay_file"
cmp "$network_policy_file" "$network_policy_replay_file"
network_policy_conflict_file="$smoke_directory/network-policy-conflict.json"
network_policy_conflict_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request PUT \
  --header "X-Request-ID: compose-smoke-network-policy-conflict" \
  --header "Idempotency-Key: compose-smoke-network-policy-conflict" \
  --header "Content-Type: application/json" --data "$network_policy_body" \
  --output "$network_policy_conflict_file" --write-out '%{http_code}' \
  "https://$endpoint$network_policy_path")
if [ "$network_policy_conflict_status" -ne 409 ] || \
  ! grep -q '"code":"NETWORK_POLICY_RESOURCE_VERSION_CONFLICT"' "$network_policy_conflict_file"; then
  echo "Compose stale Network Policy resource version was not rejected" >&2
  exit 1
fi
network_policy_list_file="$smoke_directory/network-policy-list.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/network-policies?pageSize=200" \
  compose-smoke-network-policy-list >"$network_policy_list_file"
CLOUD_AGENTS_COMPOSE_NETWORK_FILE="$network_policy_list_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_NETWORK_FILE, "utf8"));
if (value.kind !== "NetworkPolicyPage" || value.networkPolicies?.length !== 1 ||
    value.networkPolicies[0]?.metadata?.uid !== "network-compose") {
  throw new Error("Admin API Network Policy catalog drifted");
}
NODE
network_policy_audit_file="$smoke_directory/network-policy-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET "$network_policy_path/audit-events?pageSize=200" \
  compose-smoke-network-policy-audit >"$network_policy_audit_file"
CLOUD_AGENTS_COMPOSE_NETWORK_FILE="$network_policy_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_NETWORK_FILE, "utf8"));
if (value.kind !== "AdminAuditEventPage" || value.events?.length !== 1 ||
    value.events[0]?.action !== "network-policy.set" || value.events[0]?.resourceKind !== "NetworkPolicy") {
  throw new Error("Admin API Network Policy audit drifted");
}
NODE
user_admin_network_file="$smoke_directory/user-admin-network-denied.json"
user_admin_network_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-network-denied" \
  --output "$user_admin_network_file" --write-out '%{http_code}' \
  "https://$endpoint$network_policy_path")
if [ "$user_admin_network_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_network_file"; then
  echo "Compose ordinary User token was not denied by the Network Policy Admin API" >&2
  exit 1
fi
unapproved_profile_file="$smoke_directory/unapproved-profile.json"
unapproved_profile_body='{"profileId":"unapproved-profile","profileName":"unapproved-profile","version":1,"description":"Unapproved release must be rejected","providerKinds":["codex"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"network-compose","releaseDigest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","targetRefs":["docker-compose-target"],"providerCredentialRef":"provider-unapproved"}'
unapproved_profile_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-unapproved-profile" \
  --header "Idempotency-Key: compose-smoke-unapproved-profile" \
  --header "Content-Type: application/json" --data "$unapproved_profile_body" \
  --output "$unapproved_profile_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles")
if [ "$unapproved_profile_status" -ne 409 ] || \
  ! grep -q '"code":"PROFILE_VERSION_CONFLICT"' "$unapproved_profile_file"; then
  echo "Compose Profile accepted an unapproved Worker release" >&2
  exit 1
fi

profile_id=compose-docker-profile
profile_create_file="$smoke_directory/profile-create.json"
profile_create_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Docker worker profile","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"network-compose","releaseDigest":"%s","targetRefs":["%s"],"providerCredentialRef":"%s"}' \
  "$profile_id" "$profile_id" "$worker_release_digest" docker-compose-target "$target_provider_credentials_volume")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
  compose-smoke-profile-create --header "Idempotency-Key: compose-smoke-profile-create" \
  --data "$profile_create_body" >"$profile_create_file"
profile_resource_version=$(
  CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$profile_create_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
if (value.kind !== "EnvironmentProfile" || value.spec?.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.spec?.version !== 1 || value.spec?.status !== "draft" || value.metadata?.resourceVersion !== "1") {
  throw new Error("Admin API did not persist the expected draft Profile");
}
process.stdout.write(value.metadata.resourceVersion);
NODE
)
# Non-default policies can be saved but must never deploy with silently ignored restrictions.
network_deny_body='{"expectedResourceVersion":"0","policyName":"network-deny","userSummary":"No internet access","defaultEgress":"deny","allowedEgress":[],"ingressEnabled":false,"previewEnabled":false}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/network-policies/network-deny" \
  compose-smoke-network-deny --header "Idempotency-Key: compose-smoke-network-deny" \
  --data "$network_deny_body" >"$smoke_directory/network-deny.json"
network_deny_profile_body=$(CLOUD_AGENTS_COMPOSE_PROFILE_BODY="$profile_create_body" node <<'NODE'
const value = JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_BODY);
value.profileId = "network-deny-profile";
value.profileName = "network-deny-profile";
value.networkPolicyRef = "network-deny";
process.stdout.write(JSON.stringify(value));
NODE
)
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
  compose-smoke-network-deny-profile --header "Idempotency-Key: compose-smoke-network-deny-profile" \
  --data "$network_deny_profile_body" >"$smoke_directory/network-deny-profile.json"
network_deny_publish_file="$smoke_directory/network-deny-publish.json"
network_deny_publish_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-network-deny-publish" \
  --header "Idempotency-Key: compose-smoke-network-deny-publish" \
  --header "Content-Type: application/json" --data '{"expectedResourceVersion":"1"}' \
  --output "$network_deny_publish_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/network-deny-profile/versions/1:publish")
if [ "$network_deny_publish_status" -ne 409 ] || \
  ! grep -q '"code":"NETWORK_POLICY_UNAVAILABLE"' "$network_deny_publish_file"; then
  echo "Compose unsupported network enforcement did not fail closed at publication" >&2
  exit 1
fi
profile_publish_file="$smoke_directory/profile-publish.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$profile_id/versions/1:publish" \
  compose-smoke-profile-publish --header "Idempotency-Key: compose-smoke-profile-publish" \
  --data "{\"expectedResourceVersion\":\"$profile_resource_version\"}" >"$profile_publish_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$profile_publish_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
if (value.kind !== "EnvironmentProfile" || value.spec?.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.spec?.version !== 1 || value.spec?.status !== "published" || value.metadata?.resourceVersion !== "2" ||
    typeof value.spec?.publishedAt !== "string") {
  throw new Error("Admin API did not publish the Profile");
}
NODE

retry_profile_id=compose-retry-profile
retry_profile_body=$(CLOUD_AGENTS_COMPOSE_PROFILE_BODY="$profile_create_body" \
  CLOUD_AGENTS_COMPOSE_RETRY_CREDENTIAL_REF="$retry_provider_credentials_volume" node <<'NODE'
const value = JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_BODY);
value.profileId = "compose-retry-profile";
value.profileName = "compose-retry-profile";
value.description = "Recoverable deployment profile";
value.providerCredentialRef = process.env.CLOUD_AGENTS_COMPOSE_RETRY_CREDENTIAL_REF;
process.stdout.write(JSON.stringify(value));
NODE
)
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
  compose-smoke-retry-profile-create --header "Idempotency-Key: compose-smoke-retry-profile-create" \
  --data "$retry_profile_body" >"$smoke_directory/retry-profile-create.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$retry_profile_id/versions/1:publish" \
  compose-smoke-retry-profile-publish --header "Idempotency-Key: compose-smoke-retry-profile-publish" \
  --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/retry-profile-publish.json"
retry_environment_body='{"profileId":"compose-retry-profile","profileVersion":1}'
retry_environment_file="$smoke_directory/retry-environment.json"
create_user_environment_api "$retry_environment_file" compose-smoke-retry-environment-create \
  compose-smoke-retry-environment-create "$retry_environment_body"
retry_environment_id=$(CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$retry_environment_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
if (value.observedPhase !== "failed" || value.stableErrorCode !== "docker-deployment-config-unavailable") {
  throw new Error("Profile deployment failure did not persist a safe recoverable state");
}
process.stdout.write(value.environmentId);
NODE
)
retry_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$retry_environment_id" | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 0 ]; then
  echo "Compose failed Profile deployment left a target Worker" >&2
  exit 1
fi
docker volume create "$retry_provider_credentials_volume" >/dev/null
docker run --rm --user 0 --entrypoint /bin/sh \
  -v "$retry_provider_credentials_volume:/target" \
  -v "$smoke_directory/target-provider-credentials:/source:ro" \
  postgres:17.6-bookworm -ec \
  'cp /source/tenant-compose-smoke.unavailable-provider.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
recovered_environment_file="$smoke_directory/retry-environment-recovered.json"
create_user_environment_api "$recovered_environment_file" compose-smoke-retry-environment-create \
  compose-smoke-retry-environment-create "$retry_environment_body"
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$recovered_environment_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$retry_environment_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
if (value.environmentId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID || value.observedPhase !== "ready" || "stableErrorCode" in value) {
  throw new Error("Profile deployment did not recover through idempotent User API replay");
}
NODE
retry_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$retry_environment_id" | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 1 ]; then
  echo "Compose recovered Profile deployment created $retry_container_count Workers, expected 1" >&2
  exit 1
fi
retry_terminate_file="$smoke_directory/retry-environment-terminate.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$retry_environment_id:terminate" \
  compose-smoke-retry-environment-terminate --header "Idempotency-Key: compose-smoke-retry-environment-terminate" \
  --data '{"expectedGeneration":1}' >"$retry_terminate_file"
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$retry_terminate_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
const keys = Object.keys(value).sort().join("\n");
const expected = ["apiVersion", "environmentId", "expiresAt", "kind", "observedPhase", "profileId", "profileVersion", "projectRef"].sort().join("\n");
if (value.observedPhase !== "terminated" || keys !== expected) throw new Error("User environment termination response crossed the infrastructure boundary");
NODE
retry_lease_file="$smoke_directory/retry-environment-admin-lease.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-leases/$retry_environment_id" \
  compose-smoke-retry-environment-admin-get >"$retry_lease_file"
CLOUD_AGENTS_COMPOSE_LEASE_FILE="$retry_lease_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_LEASE_FILE, "utf8"));
if (value.spec?.generation !== 2 || value.spec?.observedPhase !== "terminated" || value.spec?.cleanupPhase !== "complete") {
  throw new Error("Admin Lease projection did not record recovered environment cleanup");
}
NODE
retry_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$retry_environment_id" | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 0 ]; then
  echo "Compose recovered Profile environment left a target Worker after termination" >&2
  exit 1
fi

user_admin_profile_file="$smoke_directory/user-admin-profile-denied.json"
user_admin_profile_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-profile-denied" \
  --output "$user_admin_profile_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200")
if [ "$user_admin_profile_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_profile_file"; then
  echo "Compose ordinary User token was not denied by the Profile Admin API" >&2
  exit 1
fi

published_profiles_file="$smoke_directory/published-profiles.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200" \
  compose-smoke-published-profiles >"$published_profiles_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$published_profiles_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
const profile = value.environmentProfiles?.find((item) => item.profileId === process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID);
const expectedKeys = ["apiVersion", "availability", "cpuLimitMillis", "description", "kind", "memoryLimitBytes",
  "name", "networkSummary", "profileId", "projectRef", "providerKinds", "status", "storageSummary", "version"].sort();
if (!profile || profile.kind !== "EnvironmentProfileSummary" || profile.version !== 1 ||
    profile.status !== "published" || profile.availability !== "available" ||
    profile.storageSummary !== "20 GiB managed workspace" || profile.networkSummary !== "Public internet access") {
  throw new Error("User API did not return the published Profile summary");
}
const actualKeys = Object.keys(profile).sort();
if (actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`User Profile summary field boundary changed: ${actualKeys.join(",")}`);
}
NODE

storage_policy_referenced_file="$smoke_directory/storage-policy-referenced.json"
storage_policy_referenced_body='{"expectedResourceVersion":"1","policyName":"storage-compose","userSummary":"40 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":42949672960,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"allowWorkspaceReuse":true}'
storage_policy_referenced_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request PUT \
  --header "X-Request-ID: compose-smoke-storage-policy-referenced" \
  --header "Idempotency-Key: compose-smoke-storage-policy-referenced" \
  --header "Content-Type: application/json" --data "$storage_policy_referenced_body" \
  --output "$storage_policy_referenced_file" --write-out '%{http_code}' \
  "https://$endpoint$storage_policy_path")
if [ "$storage_policy_referenced_status" -ne 409 ] || \
  ! grep -q '"code":"STORAGE_POLICY_REFERENCED"' "$storage_policy_referenced_file"; then
  echo "Compose mutated a Storage Policy already referenced by a Profile" >&2
  exit 1
fi

network_policy_referenced_file="$smoke_directory/network-policy-referenced.json"
network_policy_referenced_body='{"expectedResourceVersion":"1","policyName":"network-compose","userSummary":"Restricted access","defaultEgress":"restricted","allowedEgress":["example.com"],"ingressEnabled":false,"previewEnabled":false}'
network_policy_referenced_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request PUT \
  --header "X-Request-ID: compose-smoke-network-policy-referenced" \
  --header "Idempotency-Key: compose-smoke-network-policy-referenced" \
  --header "Content-Type: application/json" --data "$network_policy_referenced_body" \
  --output "$network_policy_referenced_file" --write-out '%{http_code}' \
  "https://$endpoint$network_policy_path")
if [ "$network_policy_referenced_status" -ne 409 ] || \
  ! grep -q '"code":"NETWORK_POLICY_REFERENCED"' "$network_policy_referenced_file"; then
  echo "Compose mutated a Network Policy already referenced by a Profile" >&2
  exit 1
fi
quota_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/lease-quota"
quota_create_file="$smoke_directory/quota-create.json"
quota_create_body='{"expectedResourceVersion":"0","maxConcurrentLeases":1,"maxCpuMillis":1000,"maxMemoryBytes":536870912,"maxLeaseTtlSeconds":3600}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-create --header "Idempotency-Key: compose-smoke-quota-create" \
  --data "$quota_create_body" >"$quota_create_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_create_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.kind !== "ProjectLeaseQuota" || value.metadata?.resourceVersion !== "1" ||
    value.spec?.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.spec?.maxConcurrentLeases !== 1 || value.spec?.maxCpuMillis !== 1000 ||
    value.spec?.maxMemoryBytes !== 536870912 || value.spec?.maxLeaseTtlSeconds !== 3600 ||
    value.status?.activeLeases !== 0 || value.status?.usedCpuMillis !== 0 ||
    value.status?.usedMemoryBytes !== 0) {
  throw new Error("Admin API did not persist the initial project Lease quota");
}
NODE
quota_replay_file="$smoke_directory/quota-replay.json"
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-create --header "Idempotency-Key: compose-smoke-quota-create" \
  --data "$quota_create_body" >"$quota_replay_file"
if ! cmp -s "$quota_create_file" "$quota_replay_file"; then
  echo "Compose project Lease quota idempotent replay drifted" >&2
  exit 1
fi
user_admin_quota_file="$smoke_directory/user-admin-quota-denied.json"
user_admin_quota_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-quota-denied" \
  --output "$user_admin_quota_file" --write-out '%{http_code}' \
  "https://$endpoint$quota_path")
if [ "$user_admin_quota_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_quota_file"; then
  echo "Compose ordinary User token was not denied by the project Lease quota Admin API" >&2
  exit 1
fi
user_quota_file="$smoke_directory/user-quota.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/lease-quota" \
  compose-smoke-user-quota >"$user_quota_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$user_quota_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
const expectedKeys = ["activeLeases", "apiVersion", "kind", "maxConcurrentLeases", "maxCpuMillis",
  "maxLeaseTtlSeconds", "maxMemoryBytes", "projectRef", "usedCpuMillis", "usedMemoryBytes"].sort();
const actualKeys = Object.keys(value).sort();
if (value.kind !== "ProjectLeaseQuotaSummary" ||
    value.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.maxConcurrentLeases !== 1 || value.activeLeases !== 0 ||
    actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`User project Lease quota boundary changed: ${actualKeys.join(",")}`);
}
for (const forbidden of ["endpoint", "credentialref", "providercredentialref", "releasedigest", "secret"]) {
  if (JSON.stringify(value).toLowerCase().includes(forbidden)) {
    throw new Error(`User project Lease quota exposed ${forbidden}`);
  }
}
NODE

user_environment_body=$(printf '{"profileId":"%s","profileVersion":1}' "$profile_id")
user_environment_file="$smoke_directory/user-environment.json"
create_user_environment_api "$user_environment_file" compose-smoke-user-environment-create \
  compose-smoke-user-environment-create "$user_environment_body"
profile_environment_id=$(CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$user_environment_file" node -e \
  'const {readFileSync}=require("node:fs");process.stdout.write(JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE,"utf8")).environmentId)')
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$user_environment_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
const expectedKeys = ["apiVersion", "environmentId", "expiresAt", "kind", "observedPhase", "profileId", "profileVersion", "projectRef"].sort();
const actualKeys = Object.keys(value).sort();
if (value.kind !== "UserEnvironment" || value.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.profileVersion !== 1 || value.observedPhase !== "ready" || actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`Profile did not create a safe ready User Environment: ${actualKeys.join(",")}`);
}
NODE
replayed_user_environment_file="$smoke_directory/user-environment-replayed.json"
create_user_environment_api "$replayed_user_environment_file" compose-smoke-user-environment-create \
  compose-smoke-user-environment-create "$user_environment_body"
if ! cmp -s "$user_environment_file" "$replayed_user_environment_file"; then
  echo "Compose Profile environment creation was not idempotent" >&2
  exit 1
fi

lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-profile-lease environment-lease get)
case "$lease_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_release_digest"'"'*'"targetId":"docker-compose-target"'*'"providerCredentialRef":"'"$target_provider_credentials_volume"'"'*'"workerEndpoint":"https://host.docker.internal:'*'"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target"'*) ;;
  *) echo "Compose Profile did not resolve to the expected ready Docker Worker: $lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Profile created $target_container_count Docker Workers, expected 1" >&2
  exit 1
fi

active_user_quota_file="$smoke_directory/active-user-quota.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/lease-quota" \
  compose-smoke-active-user-quota >"$active_user_quota_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$active_user_quota_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.activeLeases !== 1 || value.usedCpuMillis !== 1000 ||
    value.usedMemoryBytes !== 536870912 || value.maxConcurrentLeases !== 1) {
  throw new Error("User quota summary did not report the active Profile environment");
}
NODE
quota_denied_environment_file="$smoke_directory/quota-denied-environment.json"
quota_denied_environment_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-quota-denied-environment" \
  --header "Idempotency-Key: compose-smoke-quota-denied-environment" \
  --header "Content-Type: application/json" --data "$user_environment_body" \
  --output "$quota_denied_environment_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/environments")
if [ "$quota_denied_environment_status" -ne 409 ] || \
  ! grep -q '"code":"PROJECT_LEASE_COUNT_QUOTA_EXCEEDED"' "$quota_denied_environment_file"; then
  echo "Compose project Lease count quota did not reject a second environment" >&2
  exit 1
fi
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/project="$project_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose quota rejection changed the active Docker Worker count to $target_container_count" >&2
  exit 1
fi
quota_update_file="$smoke_directory/quota-update.json"
quota_update_body='{"expectedResourceVersion":"1","maxConcurrentLeases":2,"maxCpuMillis":2000,"maxMemoryBytes":1073741824,"maxLeaseTtlSeconds":3600}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-update --header "Idempotency-Key: compose-smoke-quota-update" \
  --data "$quota_update_body" >"$quota_update_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_update_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.metadata?.resourceVersion !== "2" || value.spec?.maxConcurrentLeases !== 2 ||
    value.spec?.maxCpuMillis !== 2000 || value.spec?.maxMemoryBytes !== 1073741824 ||
    value.status?.activeLeases !== 1 || value.status?.usedCpuMillis !== 1000 ||
    value.status?.usedMemoryBytes !== 536870912) {
  throw new Error("Admin API did not update project Lease quota with the resource-version fence");
}
NODE
quota_audit_file="$smoke_directory/quota-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$quota_path/audit-events?pageSize=200" compose-smoke-quota-audit >"$quota_audit_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.kind !== "AdminAuditEventPage" || value.events?.length !== 2 ||
    !value.events.every((event) => event.action === "quota.set" &&
      event.resourceKind === "ProjectLeaseQuota" && event.result === "succeeded")) {
  throw new Error("Project Lease quota did not close two Admin Audit events");
}
NODE

admin_workers_file="$smoke_directory/admin-workers.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200" \
  compose-smoke-admin-workers >"$admin_workers_file"
CLOUD_AGENTS_COMPOSE_WORKERS_FILE="$admin_workers_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_WORKER_RELEASE="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_WORKERS_FILE, "utf8"));
const worker = value.workers?.find((item) => item.metadata?.uid === process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID);
const forbidden = new Set(["endpoint", "workerendpoint", "credentialref", "providercredentialref", "prompt", "workspace", "artifact"]);
const visit = (item) => {
  if (Array.isArray(item)) return item.forEach(visit);
  if (!item || typeof item !== "object") return;
  for (const [key, nested] of Object.entries(item)) {
    if (forbidden.has(key.toLowerCase())) throw new Error(`Worker Admin API exposed forbidden field ${key}`);
    visit(nested);
  }
};
visit(value);
if (value.kind !== "WorkerPage" || value.workers?.length !== 1 || !worker ||
    worker.spec?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    worker.spec?.targetId !== "docker-compose-target" || worker.spec?.targetKind !== "docker" ||
    worker.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_WORKER_RELEASE ||
    worker.spec?.state !== "ready" || worker.spec?.cleanupPhase !== "none" ||
    worker.spec?.cpuLimitMillis !== 1000 || worker.spec?.memoryLimitBytes !== 536870912 ||
    typeof worker.spec?.lastHealthAt !== "string" || worker.spec.lastHealthAt !== worker.metadata?.updatedAt ||
    typeof worker.spec?.readyAt !== "string" || typeof worker.spec?.workerSpiffeId !== "string" ||
    typeof worker.spec?.workerServerName !== "string") {
  throw new Error("Admin API did not project the ready Docker Worker");
}
NODE

user_admin_workers_file="$smoke_directory/user-admin-workers-denied.json"
user_admin_workers_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-workers-denied" \
  --output "$user_admin_workers_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200")
if [ "$user_admin_workers_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_workers_file"; then
  echo "Compose ordinary User token was not denied by the Worker Admin API" >&2
  exit 1
fi

scheduling_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/deployment-targets/docker-compose-target"
drain_preview_file="$smoke_directory/target-drain-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path:scheduling-preview" compose-smoke-target-drain-preview >"$drain_preview_file"
drain_request_body=$(CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE="$drain_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE, "utf8"));
const lease = value.spec?.activeLeases?.[0];
if (value.kind !== "DeploymentTargetSchedulingPreview" || value.metadata?.uid !== "docker-compose-target" ||
    value.spec?.currentState !== "active" || value.spec?.desiredState !== "drained" ||
    value.spec?.expectedGeneration !== 1 || value.spec?.expectedResourceVersion !== value.metadata?.resourceVersion ||
    !/^sha256:[0-9a-f]{64}$/.test(value.spec?.impactDigest) || value.spec?.activeLeases?.length !== 1 ||
    lease?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID || lease?.observedPhase !== "ready") {
  throw new Error("Admin scheduling preview did not bind the active Docker Lease impact");
}
process.stdout.write(JSON.stringify({
  expectedGeneration: value.spec.expectedGeneration,
  expectedResourceVersion: value.spec.expectedResourceVersion,
  desiredState: value.spec.desiredState,
  impactDigest: value.spec.impactDigest,
}));
NODE
)
user_admin_scheduling_file="$smoke_directory/user-admin-scheduling-denied.json"
user_admin_scheduling_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-scheduling-denied" \
  --output "$user_admin_scheduling_file" --write-out '%{http_code}' \
  "https://$endpoint$scheduling_path:scheduling-preview")
if [ "$user_admin_scheduling_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_scheduling_file"; then
  echo "Compose ordinary User token was not denied by the Target scheduling Admin API" >&2
  exit 1
fi
drain_operation_file="$smoke_directory/target-drain-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-drain --header "Idempotency-Key: compose-smoke-target-drain" \
  --data "$drain_request_body" >"$drain_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$drain_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.kind !== "MaintenanceOperation" || value.action !== "target.drain" ||
    value.resourceId !== "docker-compose-target" || value.resourceGeneration !== 1 ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not persist a succeeded Target drain operation");
}
NODE
replayed_drain_operation_file="$smoke_directory/target-drain-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-drain --header "Idempotency-Key: compose-smoke-target-drain" \
  --data "$drain_request_body" >"$replayed_drain_operation_file"
if ! cmp -s "$drain_operation_file" "$replayed_drain_operation_file"; then
  echo "Compose Target drain was not idempotent" >&2
  exit 1
fi
drained_target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-drained-target-get target get)
case "$drained_target_output" in
  *'"schedulingState":"drained"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Target drain did not preserve ready runtime state: $drained_target_output" >&2; exit 1 ;;
esac
drained_profiles_file="$smoke_directory/drained-published-profiles.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200" \
  compose-smoke-drained-published-profiles >"$drained_profiles_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$drained_profiles_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
const profile = value.environmentProfiles?.find((item) => item.profileId === process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID);
if (profile !== undefined) throw new Error("Drained Target remained visible to User Profile selection");
NODE
drained_environment_file="$smoke_directory/drained-environment-denied.json"
drained_environment_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-drained-environment" \
  --header "Idempotency-Key: compose-smoke-drained-environment" \
  --header "Content-Type: application/json" --data "$user_environment_body" \
  --output "$drained_environment_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/environments")
if [ "$drained_environment_status" -ne 409 ] || ! grep -q '"code":"LEASE_CONFLICT"' "$drained_environment_file"; then
  echo "Compose drained Target accepted a new User Environment" >&2
  exit 1
fi
drained_replay_file="$smoke_directory/drained-existing-environment-replay.json"
create_user_environment_api "$drained_replay_file" compose-smoke-user-environment-create \
  compose-smoke-user-environment-create "$user_environment_body"
if ! cmp -s "$user_environment_file" "$drained_replay_file"; then
  echo "Compose Target drain broke an existing idempotent User Environment replay" >&2
  exit 1
fi
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Target drain changed the existing Docker Worker count" >&2
  exit 1
fi

lease_release_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-leases/$profile_environment_id"
upgrade_preview_file="$smoke_directory/lease-upgrade-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$lease_release_path:upgrade-preview?releaseDigest=sha256%3A${worker_upgrade_release_digest#sha256:}" \
  compose-smoke-lease-upgrade-preview >"$upgrade_preview_file"
upgrade_request_body=$(CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$upgrade_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE="$worker_release_digest" \
  CLOUD_AGENTS_COMPOSE_TARGET_RELEASE="$worker_upgrade_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE, "utf8"));
const spec = value.spec;
if (value.kind !== "EnvironmentLeaseUpgradePreview" || value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    spec?.action !== "upgrade" || spec.currentReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE ||
    spec.targetReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE ||
    spec.rollbackReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE || spec.rollbackGeneration !== 1 ||
    spec.expectedGeneration !== 1 || spec.expectedResourceVersion !== value.metadata.resourceVersion ||
    spec.affectedTargets !== 1 || spec.affectedWorkers !== 1 || spec.affectedLeases !== 1 ||
    !/^sha256:[0-9a-f]{64}$/.test(spec.impactDigest)) {
  throw new Error("Admin upgrade preview did not bind the selected Worker and Lease");
}
process.stdout.write(JSON.stringify({
  releaseDigest: spec.targetReleaseDigest,
  expectedGeneration: spec.expectedGeneration,
  expectedResourceVersion: spec.expectedResourceVersion,
  impactDigest: spec.impactDigest,
}));
NODE
)
user_admin_upgrade_file="$smoke_directory/user-admin-upgrade-denied.json"
user_admin_upgrade_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-denied-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-upgrade-denied" \
  --output "$user_admin_upgrade_file" --write-out '%{http_code}' \
  "https://$endpoint$lease_release_path:upgrade-preview?releaseDigest=sha256%3A${worker_upgrade_release_digest#sha256:}")
if [ "$user_admin_upgrade_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_upgrade_file"; then
  echo "Compose ordinary User token was not denied by the Lease upgrade Admin API" >&2
  exit 1
fi
upgrade_operation_file="$smoke_directory/lease-upgrade-operation.json"
upgrade_request_file="$smoke_directory/lease-upgrade-request.json"
printf '%s' "$upgrade_request_body" >"$upgrade_request_file"
admin_lease_release_operation upgrade \
  "$lease_release_path:upgrade-preview?releaseDigest=sha256%3A${worker_upgrade_release_digest#sha256:}" \
  compose-smoke-lease-upgrade compose-smoke-lease-upgrade \
  "$upgrade_request_file" "$upgrade_operation_file" "$upgrade_preview_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$upgrade_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.kind !== "MaintenanceOperation" || value.action !== "target.upgrade" ||
    value.resourceId !== "docker-compose-target" || value.resourceGeneration !== 1 ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not close the Worker upgrade operation");
}
NODE
upgrade_replay_file="$smoke_directory/lease-upgrade-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:upgrade" \
  compose-smoke-lease-upgrade --header "Idempotency-Key: compose-smoke-lease-upgrade" \
  --data-binary "@$upgrade_request_file" >"$upgrade_replay_file"
if ! cmp -s "$upgrade_operation_file" "$upgrade_replay_file"; then
  echo "Compose Worker upgrade was not idempotent" >&2
  exit 1
fi
upgraded_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-upgraded-lease environment-lease get)
case "$upgraded_lease_output" in
  *'"generation":2'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_upgrade_release_digest"'"'*) ;;
  *) echo "Compose Worker upgrade did not persist the target release: $upgraded_lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Worker upgrade left $target_container_count active Workers" >&2
  exit 1
fi
target_container_id=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id")
if [ "$(docker inspect "$target_container_id" --format '{{.Config.Image}}')" != "$worker_repository@$worker_upgrade_release_digest" ]; then
  echo "Compose Worker upgrade did not run the selected digest" >&2
  exit 1
fi

rollback_preview_file="$smoke_directory/lease-rollback-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$lease_release_path:rollback-preview" compose-smoke-lease-rollback-preview >"$rollback_preview_file"
rollback_request_body=$(CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$rollback_preview_file" \
  CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE="$worker_upgrade_release_digest" \
  CLOUD_AGENTS_COMPOSE_TARGET_RELEASE="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE, "utf8"));
const spec = value.spec;
if (spec?.action !== "rollback" || spec.currentReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE ||
    spec.targetReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE ||
    spec.rollbackReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE || spec.rollbackGeneration !== 1 ||
    spec.expectedGeneration !== 2 || spec.expectedResourceVersion !== value.metadata?.resourceVersion ||
    spec.affectedTargets !== 1 || spec.affectedWorkers !== 1 || spec.affectedLeases !== 1 ||
    !/^sha256:[0-9a-f]{64}$/.test(spec.impactDigest)) {
  throw new Error("Admin rollback preview did not bind the persisted prior release");
}
process.stdout.write(JSON.stringify({
  releaseDigest: spec.targetReleaseDigest,
  expectedGeneration: spec.expectedGeneration,
  expectedResourceVersion: spec.expectedResourceVersion,
  impactDigest: spec.impactDigest,
}));
NODE
)
rollback_operation_file="$smoke_directory/lease-rollback-operation.json"
rollback_request_file="$smoke_directory/lease-rollback-request.json"
printf '%s' "$rollback_request_body" >"$rollback_request_file"
admin_lease_release_operation rollback "$lease_release_path:rollback-preview" \
  compose-smoke-lease-rollback compose-smoke-lease-rollback \
  "$rollback_request_file" "$rollback_operation_file" "$rollback_preview_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$rollback_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.action !== "target.rollback" || value.resourceId !== "docker-compose-target" ||
    value.resourceGeneration !== 1 || value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not close the Worker rollback operation");
}
NODE
rollback_replay_file="$smoke_directory/lease-rollback-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:rollback" \
  compose-smoke-lease-rollback --header "Idempotency-Key: compose-smoke-lease-rollback" \
  --data-binary "@$rollback_request_file" >"$rollback_replay_file"
if ! cmp -s "$rollback_operation_file" "$rollback_replay_file"; then
  echo "Compose Worker rollback was not idempotent" >&2
  exit 1
fi
rolled_back_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-rolled-back-lease environment-lease get)
case "$rolled_back_lease_output" in
  *'"generation":3'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_release_digest"'"'*) ;;
  *) echo "Compose Worker rollback did not restore the prior release: $rolled_back_lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Worker rollback left $target_container_count active Workers" >&2
  exit 1
fi
target_container_id=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id")
if [ "$(docker inspect "$target_container_id" --format '{{.Config.Image}}')" != "$worker_repository@$worker_release_digest" ]; then
  echo "Compose Worker rollback did not run the prior digest" >&2
  exit 1
fi

resume_preview_file="$smoke_directory/target-resume-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path:scheduling-preview" compose-smoke-target-resume-preview >"$resume_preview_file"
resume_request_body=$(CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE="$resume_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE, "utf8"));
if (value.spec?.currentState !== "drained" || value.spec?.desiredState !== "active" ||
    value.spec?.activeLeases?.length !== 1 ||
    value.spec.activeLeases[0]?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID) {
  throw new Error("Admin scheduling preview did not bind the Target resume impact");
}
process.stdout.write(JSON.stringify({
  expectedGeneration: value.spec.expectedGeneration,
  expectedResourceVersion: value.spec.expectedResourceVersion,
  desiredState: value.spec.desiredState,
  impactDigest: value.spec.impactDigest,
}));
NODE
)
resume_operation_file="$smoke_directory/target-resume-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-resume --header "Idempotency-Key: compose-smoke-target-resume" \
  --data "$resume_request_body" >"$resume_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$resume_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.action !== "target.resume" || value.resourceId !== "docker-compose-target" ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not persist a succeeded Target resume operation");
}
NODE
resumed_target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-resumed-target-get target get)
case "$resumed_target_output" in
  *'"schedulingState":"active"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Target resume did not restore scheduling: $resumed_target_output" >&2; exit 1 ;;
esac
resumed_environment_file="$smoke_directory/resumed-user-environment.json"
create_user_environment_api "$resumed_environment_file" compose-smoke-resumed-environment \
  compose-smoke-resumed-environment "$user_environment_body"
resumed_environment_id=$(CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$resumed_environment_file" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE,"utf8"));if(value.observedPhase!=="ready")process.exit(1);process.stdout.write(value.environmentId)')
resumed_terminate_file="$smoke_directory/resumed-environment-terminate.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$resumed_environment_id:terminate" \
  compose-smoke-resumed-environment-terminate --header "Idempotency-Key: compose-smoke-resumed-environment-terminate" \
  --data '{"expectedGeneration":1}' >"$resumed_terminate_file"
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$resumed_terminate_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
if (value.observedPhase !== "terminated") throw new Error("Resumed Target environment did not terminate cleanly");
NODE

scheduling_operations_file="$smoke_directory/target-scheduling-operations.json"
scheduling_audit_file="$smoke_directory/target-scheduling-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path/operations?pageSize=200" compose-smoke-target-scheduling-operations >"$scheduling_operations_file"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path/audit-events?pageSize=200" compose-smoke-target-scheduling-audit >"$scheduling_audit_file"
CLOUD_AGENTS_COMPOSE_OPERATIONS_FILE="$scheduling_operations_file" \
  CLOUD_AGENTS_COMPOSE_AUDIT_FILE="$scheduling_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const operations = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATIONS_FILE, "utf8"));
const audit = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_AUDIT_FILE, "utf8"));
for (const action of ["target.drain", "target.upgrade", "target.rollback", "target.resume"]) {
  if (!operations.operations?.some((item) => item.action === action && item.state === "succeeded") ||
      !audit.events?.some((item) => item.action === action && item.result === "succeeded")) {
    throw new Error(`Target ${action} did not close Operation and Audit authority`);
  }
}
NODE

node "$smoke_directory/deployment/scripts/test-platform-compose-admin-web.mjs" \
	"http://$admin_web_endpoint" "$smoke_directory/admin-token" "$smoke_directory/admin-denied-token" \
	"$smoke_directory/user-token" \
	tenant-compose-smoke "$project_id" "http://$user_web_endpoint"

foundation_network_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/network-policies/network-foundation"
foundation_network_body='{"expectedResourceVersion":"0","policyName":"network-foundation","userSummary":"Private Preview without outbound network","defaultEgress":"deny","allowedEgress":[],"ingressEnabled":false,"previewEnabled":true}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$foundation_network_path" \
  compose-smoke-foundation-network --header "Idempotency-Key: compose-smoke-foundation-network" \
  --data "$foundation_network_body" >"$smoke_directory/foundation-network.json"
foundation_agent_network_id=network-agent
foundation_agent_network_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/network-policies/$foundation_agent_network_id"
foundation_agent_allowed_egress='["api.anthropic.com","api.openai.com"]'
if [ -n "$real_provider_credentials_directory" ]; then
  foundation_agent_allowed_egress=$(CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIRECTORY="$real_provider_credentials_directory" node <<'NODE'
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const directory = process.env.CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIRECTORY;
const targets = new Set();
for (const [provider, fallback] of [["codex", "api.openai.com"], ["claudeAgent", "api.anthropic.com"], ["pi", "api.openai.com"], ["deepseek-harness", "api.deepseek.com"]]) {
  const credential = JSON.parse(readFileSync(join(directory, `tenant-compose-smoke.${provider}.json`), "utf8"));
  const payload = credential.payload ?? credential;
  const baseUrl = payload.baseUrl ?? payload.baseURL;
  if (baseUrl === undefined) targets.add(fallback);
  else {
    const url = new URL(baseUrl);
    targets.add(url.hostname.replace(/^\[|\]$/g, "").toLowerCase());
  }
}
process.stdout.write(JSON.stringify([...targets].sort()));
NODE
  )
fi
foundation_agent_network_body=$(printf '{"expectedResourceVersion":"0","policyName":"network-agent","userSummary":"Agent Provider APIs","defaultEgress":"restricted","allowedEgress":%s,"ingressEnabled":false,"previewEnabled":false}' "$foundation_agent_allowed_egress")
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$foundation_agent_network_path" \
  compose-smoke-agent-network --header "Idempotency-Key: compose-smoke-agent-network" \
  --data "$foundation_agent_network_body" >"$smoke_directory/foundation-agent-network.json"
foundation_release_digest="sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5"
foundation_profile_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/runtime-profiles"
foundation_profile_body=$(printf '{"profileId":"compose-gateway-profile","profileName":"compose-gateway-profile","version":1,"description":"Packaged Access Gateway profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"docker-compose-target","networkPolicyRef":"network-foundation","imageUri":"node@%s","releaseDigest":"%s","cpuMillis":500,"memoryBytes":536870912}' \
  "$foundation_release_digest" "$foundation_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
  compose-smoke-foundation-profile --header "Idempotency-Key: compose-smoke-foundation-profile" \
  --data "$foundation_profile_body" >"$smoke_directory/foundation-profile.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "$foundation_profile_path/compose-gateway-profile/versions/1:publish" \
  compose-smoke-foundation-profile-publish \
  --header "Idempotency-Key: compose-smoke-foundation-profile-publish" \
  --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/foundation-profile-published.json"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$smoke_directory/foundation-profile-published.json" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
if (value.kind !== "RuntimeProfile" || value.spec?.status !== "published" ||
    value.spec?.targetId !== "docker-compose-target" || value.spec?.networkPolicyRef !== "network-foundation") {
  throw new Error("Compose did not publish the Foundation RuntimeProfile");
}
NODE
foundation_agent_profile_id=compose-agent-profile
foundation_agent_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Agent Runtime profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"docker-compose-target","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":536870912}' \
  "$foundation_agent_profile_id" "$foundation_agent_profile_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
  compose-smoke-agent-profile --header "Idempotency-Key: compose-smoke-agent-profile" \
  --data "$foundation_agent_profile_body" >"$smoke_directory/foundation-agent-profile.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "$foundation_profile_path/$foundation_agent_profile_id/versions/1:publish" \
  compose-smoke-agent-profile-publish \
  --header "Idempotency-Key: compose-smoke-agent-profile-publish" \
  --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/foundation-agent-profile-published.json"
foundation_agent_target_refs='["docker-compose-target"]'
if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = docker ]; then
  foundation_agent_restore_profile_id=compose-agent-profile-restore
  foundation_agent_restore_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Agent Runtime restore profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"docker-compose-target-restore","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":536870912}' \
    "$foundation_agent_restore_profile_id" "$foundation_agent_restore_profile_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
    compose-smoke-agent-restore-profile --header "Idempotency-Key: compose-smoke-agent-restore-profile" \
    --data "$foundation_agent_restore_profile_body" >"$smoke_directory/foundation-agent-restore-profile.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "$foundation_profile_path/$foundation_agent_restore_profile_id/versions/1:publish" \
    compose-smoke-agent-restore-profile-publish \
    --header "Idempotency-Key: compose-smoke-agent-restore-profile-publish" \
    --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/foundation-agent-restore-profile-published.json"
  foundation_agent_target_refs='["docker-compose-target","docker-compose-target-restore"]'
fi
foundation_agent_environment_profile_id=compose-agent-environment-profile
foundation_agent_environment_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Agent Foundation profile","providerKinds":["codex","claudeAgent","pi","deepseek-harness"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"%s","releaseDigest":"%s","targetRefs":%s,"providerCredentialRef":"%s"}' \
  "$foundation_agent_environment_profile_id" "$foundation_agent_environment_profile_id" "$foundation_agent_network_id" "$worker_release_digest" "$foundation_agent_target_refs" "$target_provider_credentials_volume")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
  compose-smoke-agent-environment-profile --header "Idempotency-Key: compose-smoke-agent-environment-profile" \
  --data "$foundation_agent_environment_profile_body" >"$smoke_directory/foundation-agent-environment-profile.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$foundation_agent_environment_profile_id/versions/1:publish" \
  compose-smoke-agent-environment-profile-publish \
  --header "Idempotency-Key: compose-smoke-agent-environment-profile-publish" \
  --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/foundation-agent-environment-profile-published.json"
if [ "$kubernetes_runtime" -eq 1 ]; then
  kubernetes_agent_runtime_profile_id=compose-kubernetes-agent-profile
  kubernetes_agent_runtime_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Kubernetes Agent Runtime profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"%s","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":1073741824}' \
    "$kubernetes_agent_runtime_profile_id" "$kubernetes_agent_runtime_profile_id" "$kubernetes_runtime_target_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
    compose-kubernetes-agent-profile --header "Idempotency-Key: compose-kubernetes-agent-profile" \
    --data "$kubernetes_agent_runtime_profile_body" >"$smoke_directory/kubernetes-agent-profile.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "$foundation_profile_path/$kubernetes_agent_runtime_profile_id/versions/1:publish" \
    compose-kubernetes-agent-profile-publish --header "Idempotency-Key: compose-kubernetes-agent-profile-publish" \
    --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/kubernetes-agent-profile-published.json"
  kubernetes_agent_target_refs=$(printf '["%s"]' "$kubernetes_runtime_target_id")
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_agent_target_refs=$(printf '["%s","%s"]' "$kubernetes_runtime_target_id" "$kubernetes_destination_target_id")
  fi
  kubernetes_agent_environment_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Kubernetes Agent Foundation profile","providerKinds":["codex","claudeAgent","pi","deepseek-harness"],"cpuLimitMillis":1000,"memoryLimitBytes":1073741824,"storagePolicyRef":"storage-compose","networkPolicyRef":"%s","releaseDigest":"%s","targetRefs":%s,"providerCredentialRef":"%s"}' \
    "$kubernetes_agent_environment_profile_id" "$kubernetes_agent_environment_profile_id" "$foundation_agent_network_id" "$worker_release_digest" "$kubernetes_agent_target_refs" "$target_provider_credentials_volume")
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
    compose-kubernetes-agent-environment-profile --header "Idempotency-Key: compose-kubernetes-agent-environment-profile" \
    --data "$kubernetes_agent_environment_profile_body" >"$smoke_directory/kubernetes-agent-environment-profile.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$kubernetes_agent_environment_profile_id/versions/1:publish" \
    compose-kubernetes-agent-environment-profile-publish --header "Idempotency-Key: compose-kubernetes-agent-environment-profile-publish" \
    --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/kubernetes-agent-environment-profile-published.json"
  if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ]; then
    kubernetes_agent_restore_profile_id=compose-kubernetes-agent-restore-profile
    kubernetes_agent_restore_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Kubernetes Agent restore profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"%s","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":1073741824}' \
      "$kubernetes_agent_restore_profile_id" "$kubernetes_agent_restore_profile_id" "$kubernetes_destination_target_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
    control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
      compose-kubernetes-agent-restore-profile --header "Idempotency-Key: compose-kubernetes-agent-restore-profile" \
      --data "$kubernetes_agent_restore_profile_body" >"$smoke_directory/kubernetes-agent-restore-profile.json"
    control_plane_api "$smoke_directory/admin-curl.conf" POST \
      "$foundation_profile_path/$kubernetes_agent_restore_profile_id/versions/1:publish" \
      compose-kubernetes-agent-restore-profile-publish --header "Idempotency-Key: compose-kubernetes-agent-restore-profile-publish" \
      --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/kubernetes-agent-restore-profile-published.json"
  fi
fi
foundation_sandbox_id=compose-gateway-sandbox
foundation_sandbox_path="/v1/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions"
foundation_sandbox_body=$(printf '{"workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"compose-gateway-profile","runtimeProfileVersion":1,"ttlSeconds":600}' \
  "$foundation_workspace_id" "$foundation_workspace_id" "$foundation_sandbox_id")
submit_foundation_operation_api "$smoke_directory/user-curl.conf" "$foundation_sandbox_path" \
  compose-smoke-foundation-sandbox compose-smoke-foundation-sandbox "$foundation_sandbox_body" \
  "$smoke_directory/foundation-sandbox-created.json"
foundation_admin_sandbox_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$foundation_sandbox_id"
attempt=0
while :; do
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_admin_sandbox_path" \
    compose-smoke-foundation-sandbox-get >"$smoke_directory/foundation-sandbox.json"
  foundation_sandbox_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-sandbox.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
  foundation_sandbox_state=${foundation_sandbox_values%%|*}
  foundation_sandbox_rest=${foundation_sandbox_values#*|}
  foundation_sandbox_generation=${foundation_sandbox_rest%%|*}
  foundation_sandbox_rest=${foundation_sandbox_rest#*|}
  foundation_sandbox_resource_version=${foundation_sandbox_rest%%|*}
  foundation_sandbox_error=${foundation_sandbox_rest#*|}
  if [ "$foundation_sandbox_state" = running ]; then
    break
  fi
  if [ "$foundation_sandbox_state" = failed ]; then
    echo "Compose Foundation Sandbox failed: $foundation_sandbox_error" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 120 ]; then
    echo "Compose Foundation Sandbox did not become ready: state=$foundation_sandbox_state error=$foundation_sandbox_error" >&2
    exit 1
  fi
  sleep 1
done

foundation_grant_file="$smoke_directory/foundation-grant.json"
cloud_agentsctl_user --project "$project_id" --sandbox "$foundation_sandbox_id" \
  --request-id compose-smoke-foundation-grant --idempotency-key compose-smoke-foundation-grant \
  sandbox grant --expected-generation "$foundation_sandbox_generation" --ttl-seconds 300 >"$foundation_grant_file"
foundation_grant_values=$(CLOUD_AGENTS_COMPOSE_GRANT_FILE="$foundation_grant_file" \
  CLOUD_AGENTS_COMPOSE_STATE="$smoke_directory" node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_GRANT_FILE, "utf8"));
if (value.kind !== "SandboxAccessGrant" || !value.grantId || !value.accessToken || !value.sshUsername) {
  throw new Error("Compose did not issue a bounded Sandbox access Grant");
}
writeFileSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-grant-token`, `${value.accessToken}\n`, { mode: 0o600 });
writeFileSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-curl.conf`, `header = "Authorization: Bearer ${value.accessToken}"\n`, { mode: 0o600 });
chmodSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-grant-token`, 0o600);
chmodSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-curl.conf`, 0o600);
process.stdout.write(`${value.grantId}|${value.sshUsername}`);
NODE
)
foundation_grant_id=${foundation_grant_values%%|*}
foundation_ssh_username=${foundation_grant_values#*|}

cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-file-write files write --path gateway-proof.txt \
  --content-base64url Z2F0ZXdheS1maWxlCg >"$smoke_directory/gateway-file-write.json"
gateway_file_read=$(cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-file-read files read --path gateway-proof.txt)
case "$gateway_file_read" in
  *'"contentBase64Url":"Z2F0ZXdheS1maWxlCg"'*) ;;
  *) echo "Compose Access Gateway Files route changed: $gateway_file_read" >&2; exit 1 ;;
esac

pty_create_file="$smoke_directory/gateway-pty-create.json"
cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-pty-create pty create >"$pty_create_file"
foundation_pty_id=$(CLOUD_AGENTS_COMPOSE_PTY_FILE="$pty_create_file" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PTY_FILE,"utf8"));if(value.kind!=="SandboxPTYSession"||!value.sessionId)process.exit(1);process.stdout.write(value.sessionId)')
gateway_pty_output=$(printf 'pwd; printf "gateway-pty-ok\\n"; exit\n' | \
  cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
    --pty-session "$foundation_pty_id" --request-id compose-smoke-gateway-pty-attach \
    pty attach --takeover)
case "$gateway_pty_output" in
  *'/workspace'*'gateway-pty-ok'*) ;;
  *) echo "Compose Access Gateway PTY route changed: $gateway_pty_output" >&2; exit 1 ;;
esac
cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --pty-session "$foundation_pty_id" --request-id compose-smoke-gateway-pty-delete \
  pty delete >/dev/null

preview_source='require("http").createServer((_,response)=>response.end("gateway-preview-ok\n")).listen(3000,"0.0.0.0")'
preview_content=$(CLOUD_AGENTS_COMPOSE_PREVIEW_SOURCE="$preview_source" node -e \
  'process.stdout.write(Buffer.from(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_SOURCE).toString("base64url"))')
cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-preview-file files write --path gateway-preview.js \
  --content-base64url "$preview_content" >/dev/null
cloud_agentsctl_user --timeout 60s --project "$project_id" --sandbox "$foundation_sandbox_id" \
  --request-id compose-smoke-gateway-preview-start sandbox exec \
  --expected-generation "$foundation_sandbox_generation" \
  --command 'node /workspace/gateway-preview.js >/tmp/gateway-preview.log 2>&1 &' >/dev/null
preview_file="$smoke_directory/gateway-preview.json"
cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-preview-register preview register --port 3000 >"$preview_file"
foundation_preview_path=$(CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$preview_file" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE,"utf8"));if(value.kind!=="SandboxPreviewPort"||!value.proxyPath)process.exit(1);process.stdout.write(value.proxyPath)')
attempt=0
gateway_preview_body_file="$smoke_directory/gateway-preview-body"
while :; do
  if ! gateway_preview_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
    --header "X-Request-ID: compose-smoke-gateway-preview-read" \
    --config "$smoke_directory/gateway-curl.conf" --output "$gateway_preview_body_file" \
    --write-out '%{http_code}' "https://$gateway_endpoint$foundation_preview_path"); then
    gateway_preview_status=transport-error
  fi
  if [ "$gateway_preview_status" = 200 ] && [ "$(cat "$gateway_preview_body_file")" = "gateway-preview-ok" ]; then
    break
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "Compose Access Gateway Preview route did not become ready: status=$gateway_preview_status" >&2
    head -c 500 "$gateway_preview_body_file" >&2 || true
    echo >&2
    exit 1
  fi
  sleep 1
done
gateway_wrong_token_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-preview-wrong-token" \
  --header "Authorization: Bearer cag1_0000000000000000000000000000000000000000000" \
  --output "$smoke_directory/gateway-wrong-token.json" --write-out '%{http_code}' \
  "https://$gateway_endpoint$foundation_preview_path")
test "$gateway_wrong_token_status" -eq 403
gateway_cross_tenant_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-cross-tenant" \
  --config "$smoke_directory/gateway-curl.conf" --output "$smoke_directory/gateway-cross-tenant.json" \
  --write-out '%{http_code}' \
  "https://$gateway_endpoint/v1/tenants/tenant-other/projects/$project_id/sandbox-access-grants/$foundation_grant_id/files?path=.")
test "$gateway_cross_tenant_status" -eq 403

expired_grant_file="$smoke_directory/foundation-grant-expiring.json"
cloud_agentsctl_user --project "$project_id" --sandbox "$foundation_sandbox_id" \
  --request-id compose-smoke-foundation-grant-expiring --idempotency-key compose-smoke-foundation-grant-expiring \
  sandbox grant --expected-generation "$foundation_sandbox_generation" --ttl-seconds 60 >"$expired_grant_file"
expired_grant_values=$(CLOUD_AGENTS_COMPOSE_GRANT_FILE="$expired_grant_file" \
  CLOUD_AGENTS_COMPOSE_STATE="$smoke_directory" node <<'NODE'
const { chmodSync, readFileSync, writeFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_GRANT_FILE, "utf8"));
if (value.kind !== "SandboxAccessGrant" || !value.grantId || !value.accessToken) {
  throw new Error("Compose did not issue the expiring Sandbox access Grant");
}
writeFileSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-expiring-curl.conf`, `header = "Authorization: Bearer ${value.accessToken}"\n`, { mode: 0o600 });
chmodSync(`${process.env.CLOUD_AGENTS_COMPOSE_STATE}/gateway-expiring-curl.conf`, 0o600);
process.stdout.write(value.grantId);
NODE
)
sleep 61
gateway_expired_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-expired" \
  --config "$smoke_directory/gateway-expiring-curl.conf" --output "$smoke_directory/gateway-expired.json" \
  --write-out '%{http_code}' \
  "https://$gateway_endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/sandbox-access-grants/$expired_grant_values/files?path=.")
test "$gateway_expired_status" -eq 403
control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_admin_sandbox_path/access-grants?pageSize=50" \
  compose-smoke-foundation-grants-expired >"$smoke_directory/foundation-grants-expired.json"
CLOUD_AGENTS_COMPOSE_GRANTS_FILE="$smoke_directory/foundation-grants-expired.json" \
  CLOUD_AGENTS_COMPOSE_GRANT_ID="$expired_grant_values" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_GRANTS_FILE,"utf8"));const grant=value.accessGrants?.find(({metadata})=>metadata?.uid===process.env.CLOUD_AGENTS_COMPOSE_GRANT_ID);if(grant?.spec?.status!=="expired")process.exit(1)'
printf 'expired_grant_negative=passed\n'

foundation_ssh_public_key=$(ssh-keygen -y -f "$smoke_directory/access-gateway-ssh-host-key")
printf '[127.0.0.1]:%s %s\n' "$gateway_ssh_port" "$foundation_ssh_public_key" >"$smoke_directory/gateway-known-hosts"
chmod 0600 "$smoke_directory/gateway-known-hosts"
gateway_ssh() {
  DISPLAY=cloud-agents-compose-smoke SSH_ASKPASS_REQUIRE=force \
    SSH_ASKPASS="$smoke_directory/ssh-askpass.sh" \
    CLOUD_AGENTS_GATEWAY_PASSWORD="$(cat "$smoke_directory/gateway-grant-token")" \
    ssh -F /dev/null -tt -o BatchMode=no -o IdentitiesOnly=yes -o PasswordAuthentication=yes \
      -o PubkeyAuthentication=no -o KbdInteractiveAuthentication=no -o LogLevel=ERROR \
      -o StrictHostKeyChecking=yes -o UserKnownHostsFile="$smoke_directory/gateway-known-hosts" \
      -p "$gateway_ssh_port" "$foundation_ssh_username@127.0.0.1" \
      'pwd; printf "gateway-ssh-ok\n"; sleep 0.1'
}
if ! gateway_ssh_output=$(gateway_ssh 2>"$smoke_directory/gateway-ssh.err"); then
  echo "Compose Access Gateway SSH command failed" >&2
  cat "$smoke_directory/gateway-ssh.err" >&2
  exit 1
fi
case "$gateway_ssh_output" in
  *'/workspace'*'gateway-ssh-ok'*) ;;
  *)
    echo "Compose Access Gateway SSH route changed: $gateway_ssh_output" >&2
    cat "$smoke_directory/gateway-ssh.err" >&2
    exit 1 ;;
esac

compose restart access-gateway >/dev/null
wait_gateway
printf '[127.0.0.1]:%s %s\n' "$gateway_ssh_port" "$foundation_ssh_public_key" >"$smoke_directory/gateway-known-hosts"
gateway_file_replay=$(cloud_agentsctl_gateway --project "$project_id" --grant "$foundation_grant_id" \
  --request-id compose-smoke-gateway-file-restart files read --path gateway-proof.txt)
case "$gateway_file_replay" in
  *'"contentBase64Url":"Z2F0ZXdheS1maWxlCg"'*) ;;
  *) echo "Compose Access Gateway restart lost the Files route" >&2; exit 1 ;;
esac
test "$(curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-preview-restart" \
  --config "$smoke_directory/gateway-curl.conf" "https://$gateway_endpoint$foundation_preview_path")" = "gateway-preview-ok"
if ! gateway_ssh_output=$(gateway_ssh 2>"$smoke_directory/gateway-ssh-restart.err"); then
  echo "Compose Access Gateway restart SSH command failed" >&2
  cat "$smoke_directory/gateway-ssh-restart.err" >&2
  exit 1
fi
case "$gateway_ssh_output" in
  *'/workspace'*'gateway-ssh-ok'*) ;;
  *)
    echo "Compose Access Gateway restart lost the SSH route: $gateway_ssh_output" >&2
    cat "$smoke_directory/gateway-ssh-restart.err" >&2
    exit 1 ;;
esac

foundation_grants_path="$foundation_admin_sandbox_path/access-grants?pageSize=50"
control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_grants_path" \
  compose-smoke-foundation-grants >"$smoke_directory/foundation-grants.json"
foundation_grant_resource_version=$(CLOUD_AGENTS_COMPOSE_GRANTS_FILE="$smoke_directory/foundation-grants.json" \
  CLOUD_AGENTS_COMPOSE_GRANT_ID="$foundation_grant_id" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_GRANTS_FILE,"utf8"));const grant=value.accessGrants?.find(({metadata})=>metadata?.uid===process.env.CLOUD_AGENTS_COMPOSE_GRANT_ID);if(!grant?.metadata?.resourceVersion||grant.spec?.status!=="active")process.exit(1);process.stdout.write(grant.metadata.resourceVersion)')
foundation_revoke_grant_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedGrantId":"%s"}' \
  "$foundation_sandbox_generation" "$foundation_grant_resource_version" "$foundation_grant_id")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "$foundation_admin_sandbox_path/access-grants/$foundation_grant_id:revoke" \
  compose-smoke-foundation-grant-revoke \
  --header "Idempotency-Key: compose-smoke-foundation-grant-revoke" \
  --data "$foundation_revoke_grant_body" >"$smoke_directory/foundation-grant-revoked.json"
CLOUD_AGENTS_COMPOSE_GRANT_FILE="$smoke_directory/foundation-grant-revoked.json" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_GRANT_FILE,"utf8"));if(value.spec?.status!=="revoked"||!value.spec?.revokedAt)process.exit(1)'

gateway_revoked_file_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-file-revoked" \
  --config "$smoke_directory/gateway-curl.conf" --output "$smoke_directory/gateway-file-revoked.json" \
  --write-out '%{http_code}' \
  "https://$gateway_endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/sandbox-access-grants/$foundation_grant_id/files/content?path=gateway-proof.txt&offset=0&limit=1024")
test "$gateway_revoked_file_status" -eq 403
gateway_revoked_preview_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --header "X-Request-ID: compose-smoke-gateway-preview-revoked" \
  --config "$smoke_directory/gateway-curl.conf" --output "$smoke_directory/gateway-preview-revoked.json" \
  --write-out '%{http_code}' "https://$gateway_endpoint$foundation_preview_path")
test "$gateway_revoked_preview_status" -eq 403
if gateway_ssh >"$smoke_directory/gateway-ssh-revoked.out" 2>"$smoke_directory/gateway-ssh-revoked.err"; then
  echo "Compose Access Gateway accepted a revoked Grant over SSH" >&2
  exit 1
fi

control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_admin_sandbox_path" \
  compose-smoke-foundation-sandbox-before-stop >"$smoke_directory/foundation-sandbox-before-stop.json"
foundation_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-sandbox-before-stop.json" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)')
foundation_stop_generation=${foundation_stop_values%%|*}
foundation_stop_resource_version=${foundation_stop_values#*|}
foundation_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
  "$foundation_stop_generation" "$foundation_stop_resource_version" "$foundation_sandbox_id")
control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_admin_sandbox_path:stop" \
  compose-smoke-foundation-sandbox-stop \
  --header "Idempotency-Key: compose-smoke-foundation-sandbox-stop" \
  --data "$foundation_stop_body" >"$smoke_directory/foundation-sandbox-stop.json"
attempt=0
while :; do
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_admin_sandbox_path" \
    compose-smoke-foundation-sandbox-stopped >"$smoke_directory/foundation-sandbox.json"
  if CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-sandbox.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.exit(value.spec?.observedState==="stopped"&&value.spec?.writerReleased===true&&value.spec?.networkPolicyEnforcement==="stopped"?0:1)'; then
    break
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 120 ]; then
    echo "Compose Foundation Sandbox did not stop" >&2
    exit 1
  fi
  sleep 1
done
test -z "$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/project="$project_id" \
  --filter label=cloud-agents.dev/workspace="$foundation_workspace_id")"
docker ps -aq --filter label=opensandbox.io/id | sort >"$smoke_directory/opensandbox-runtime-current"
docker ps -aq --filter label=opensandbox.io/egress-sidecar-for | sort >"$smoke_directory/opensandbox-egress-current"
docker volume ls -q --filter label=opensandbox.io/volume-managed-by=server | sort >"$smoke_directory/opensandbox-volume-current"
cmp -s "$smoke_directory/opensandbox-runtime-baseline" "$smoke_directory/opensandbox-runtime-current"
cmp -s "$smoke_directory/opensandbox-egress-baseline" "$smoke_directory/opensandbox-egress-current"
cmp -s "$smoke_directory/opensandbox-volume-baseline" "$smoke_directory/opensandbox-volume-current"
foundation_volume=$(docker volume ls -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/project="$project_id" \
  --filter label=cloud-agents.dev/workspace="$foundation_workspace_id")
case "$foundation_volume" in
  '' | *' '*) echo "Compose Foundation Workspace volume inventory changed" >&2; exit 1 ;;
esac
docker volume rm "$foundation_volume" >/dev/null

foundation_agent_sandbox_body=$(printf '{"workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":7200}' \
  "$foundation_agent_workspace_id" "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" "$foundation_agent_profile_id")
submit_foundation_operation_api "$smoke_directory/user-curl.conf" "$foundation_sandbox_path" \
  compose-smoke-agent-sandbox compose-smoke-agent-sandbox "$foundation_agent_sandbox_body" \
  "$smoke_directory/foundation-agent-sandbox-created.json"
foundation_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$foundation_agent_sandbox_id"
attempt=0
while :; do
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
    compose-smoke-agent-sandbox-get >"$smoke_directory/foundation-agent-sandbox.json"
  foundation_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-agent-sandbox.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
  foundation_agent_state=${foundation_agent_values%%|*}
  foundation_agent_rest=${foundation_agent_values#*|}
  foundation_agent_generation=${foundation_agent_rest%%|*}
  foundation_agent_rest=${foundation_agent_rest#*|}
  foundation_agent_resource_version=${foundation_agent_rest%%|*}
  foundation_agent_rest=${foundation_agent_rest#*|}
  foundation_agent_runtime_id=${foundation_agent_rest%%|*}
  foundation_agent_error=${foundation_agent_rest#*|}
  if [ "$foundation_agent_state" = running ]; then
    [ -n "$foundation_agent_runtime_id" ] || { echo "Compose Agent Sandbox is running without a Runtime id" >&2; exit 1; }
    break
  fi
  if [ "$foundation_agent_state" = failed ]; then
    echo "Compose Agent Sandbox failed: $foundation_agent_error" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 120 ]; then
    echo "Compose Agent Sandbox did not become ready: state=$foundation_agent_state error=$foundation_agent_error" >&2
    exit 1
  fi
  sleep 1
done

remote_agent_workspace_id=
remote_agent_sandbox_id=
remote_agent_generation=
remote_agent_resource_version=
remote_agent_environment_profile_id=
if [ "$remote_runtime" -eq 1 ]; then
  remote_agent_workspace_id=compose-remote-agent-workspace
  remote_agent_sandbox_id=compose-remote-agent-sandbox
  remote_agent_runtime_profile_id=compose-remote-agent-profile
  remote_agent_environment_profile_id=compose-remote-agent-environment-profile
  remote_agent_runtime_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Outbound RemoteWorker Agent Runtime profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"%s","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":536870912}' \
    "$remote_agent_runtime_profile_id" "$remote_agent_runtime_profile_id" "$remote_target_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
    compose-remote-agent-profile --header "Idempotency-Key: compose-remote-agent-profile" \
    --data "$remote_agent_runtime_profile_body" >"$smoke_directory/remote-agent-profile.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "$foundation_profile_path/$remote_agent_runtime_profile_id/versions/1:publish" \
    compose-remote-agent-profile-publish --header "Idempotency-Key: compose-remote-agent-profile-publish" \
    --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/remote-agent-profile-published.json"
  remote_agent_target_refs=$(printf '["%s"]' "$remote_target_id")
  if [ -n "$remote_target_restore_id" ]; then
    remote_agent_restore_profile_id=compose-remote-agent-restore-profile
    remote_agent_restore_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Outbound RemoteWorker Agent restore profile","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"%s","networkPolicyRef":"%s","imageUri":"%s@%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":536870912}' \
      "$remote_agent_restore_profile_id" "$remote_agent_restore_profile_id" "$remote_target_restore_id" "$foundation_agent_network_id" "$worker_repository" "$worker_release_digest" "$worker_release_digest")
    control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_profile_path" \
      compose-remote-agent-restore-profile --header "Idempotency-Key: compose-remote-agent-restore-profile" \
      --data "$remote_agent_restore_profile_body" >"$smoke_directory/remote-agent-restore-profile.json"
    control_plane_api "$smoke_directory/admin-curl.conf" POST \
      "$foundation_profile_path/$remote_agent_restore_profile_id/versions/1:publish" \
      compose-remote-agent-restore-profile-publish --header "Idempotency-Key: compose-remote-agent-restore-profile-publish" \
      --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/remote-agent-restore-profile-published.json"
    remote_agent_target_refs=$(printf '["%s","%s"]' "$remote_target_id" "$remote_target_restore_id")
  fi
  remote_agent_environment_profile_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Outbound RemoteWorker Agent Foundation profile","providerKinds":["codex","claudeAgent","pi","deepseek-harness"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"%s","releaseDigest":"%s","targetRefs":%s,"providerCredentialRef":"%s"}' \
    "$remote_agent_environment_profile_id" "$remote_agent_environment_profile_id" "$foundation_agent_network_id" "$worker_release_digest" "$remote_agent_target_refs" "$target_provider_credentials_volume")
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
    compose-remote-agent-environment-profile --header "Idempotency-Key: compose-remote-agent-environment-profile" \
    --data "$remote_agent_environment_profile_body" >"$smoke_directory/remote-agent-environment-profile.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$remote_agent_environment_profile_id/versions/1:publish" \
    compose-remote-agent-environment-profile-publish --header "Idempotency-Key: compose-remote-agent-environment-profile-publish" \
    --data '{"expectedResourceVersion":"1"}' >"$smoke_directory/remote-agent-environment-profile-published.json"
  remote_agent_sandbox_body=$(printf '{"workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":7200}' \
    "$remote_agent_workspace_id" "$remote_agent_workspace_id" "$remote_agent_sandbox_id" "$remote_agent_runtime_profile_id")
  submit_foundation_operation_api "$smoke_directory/user-curl.conf" "$foundation_sandbox_path" \
    compose-remote-agent-sandbox compose-remote-agent-sandbox "$remote_agent_sandbox_body" \
    "$smoke_directory/remote-agent-sandbox-created.json"
  remote_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$remote_agent_sandbox_id"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$remote_agent_admin_path" \
      compose-remote-agent-sandbox-get >"$smoke_directory/remote-agent-sandbox.json"
    remote_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-agent-sandbox.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
    remote_agent_state=${remote_agent_values%%|*}
    remote_agent_rest=${remote_agent_values#*|}
    remote_agent_generation=${remote_agent_rest%%|*}
    remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_resource_version=${remote_agent_rest%%|*}
    remote_agent_error=${remote_agent_rest#*|}
    if [ "$remote_agent_state" = running ]; then break; fi
    if [ "$remote_agent_state" = failed ]; then
      echo "Compose outbound RemoteWorker Agent Sandbox failed: $remote_agent_error" >&2
      exit 1
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Compose outbound RemoteWorker Agent Sandbox did not become ready: state=$remote_agent_state error=$remote_agent_error" >&2
      docker logs "$remote_worker_container" >&2 || true
      exit 1
    fi
    sleep 1
  done
fi

if [ "$kubernetes_runtime" -eq 1 ]; then
  kubernetes_bind_workspace_volume() {
    bind_namespace=$1
    bind_volume_name=$2
    bind_node_name=$3
    kubernetes_ctl -n "$bind_namespace" delete pod ca-workspace-binder --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || true
    kubernetes_ctl -n "$bind_namespace" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ca-workspace-binder
  labels:
    cloud-agents.dev/test-run: $project
spec:
  restartPolicy: Never
  nodeSelector:
    kubernetes.io/hostname: $bind_node_name
  containers:
    - name: binder
      image: $worker_repository@$worker_release_digest
      imagePullPolicy: Never
      command: ["/bin/sh", "-c", "true"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  volumes:
    - name: workspace
      persistentVolumeClaim:
        claimName: $bind_volume_name
EOF
    kubernetes_ctl -n "$bind_namespace" wait pod/ca-workspace-binder \
      --for=jsonpath='{.status.phase}'=Succeeded --timeout=120s >/dev/null
    kubernetes_ctl -n "$bind_namespace" delete pod ca-workspace-binder --wait=true --timeout=60s >/dev/null
  }
  kubernetes_agent_sandbox_body=$(printf '{"workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":7200}' \
    "$kubernetes_agent_workspace_id" "$kubernetes_agent_workspace_id" "$kubernetes_agent_sandbox_id" "$kubernetes_agent_runtime_profile_id")
  submit_foundation_operation_api "$smoke_directory/user-curl.conf" "$foundation_sandbox_path" \
    compose-kubernetes-agent-sandbox compose-kubernetes-agent-sandbox "$kubernetes_agent_sandbox_body" \
    "$smoke_directory/kubernetes-agent-sandbox-created.json"
  kubernetes_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$kubernetes_agent_sandbox_id"
  kubernetes_source_volume_bound=0
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$kubernetes_agent_admin_path" \
      compose-kubernetes-agent-sandbox-get >"$smoke_directory/kubernetes-agent-sandbox.json"
    kubernetes_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-agent-sandbox.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
    kubernetes_agent_state=${kubernetes_agent_values%%|*}
    kubernetes_agent_rest=${kubernetes_agent_values#*|}
    kubernetes_agent_generation=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_resource_version=${kubernetes_agent_rest%%|*}
    kubernetes_agent_error=${kubernetes_agent_rest#*|}
    if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ] && [ "$kubernetes_source_volume_bound" -eq 0 ]; then
      kubernetes_agent_volume=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-agent-sandbox.json" node -e \
        'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(value.spec?.physicalVolumeId??"")')
      if [ -n "$kubernetes_agent_volume" ]; then
        kubernetes_bind_workspace_volume "$kubernetes_runtime_namespace" "$kubernetes_agent_volume" "$kubernetes_source_node"
        kubernetes_source_volume_bound=1
      fi
    fi
    if [ "$kubernetes_agent_state" = running ]; then break; fi
    if [ "$kubernetes_agent_state" = failed ]; then
      echo "Compose Kubernetes Agent Sandbox failed: $kubernetes_agent_error" >&2
      exit 1
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 240 ]; then
      echo "Compose Kubernetes Agent Sandbox did not become ready: state=$kubernetes_agent_state error=$kubernetes_agent_error" >&2
      kubernetes_ctl -n "$kubernetes_runtime_namespace" get pods,batchsandboxes.sandbox.opensandbox.io,pvc,events -o wide >&2 || true
      exit 1
    fi
    sleep 1
  done
fi

codex_session_output=$(cloud_agentsctl_user --project "$project_id" --lease "$profile_environment_id" \
  --session session-compose-smoke \
  --request-id compose-smoke-session-create --idempotency-key compose-smoke-session-create \
  session create --provider codex)
case "$codex_session_output" in
  *'"providerKind":"codex"'*'"environmentLeaseId":"'"$profile_environment_id"'"'*'"environmentProfileId":"'"$profile_id"'"'*'"environmentProfileVersion":1'*) ;;
  *) echo "Compose User token did not create the Profile-bound Codex Session" >&2; exit 1 ;;
esac
codex_turn_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke \
  --request-id compose-smoke-turn-create --idempotency-key compose-smoke-turn-create \
  turn create --input "verify packaged Compose Runtime")
case "$codex_turn_output" in
  *'"state":"queued"'*) ;;
  *) echo "Compose User token did not persist the Codex Turn" >&2; exit 1 ;;
esac
claude_session_output=$(cloud_agentsctl_user --project "$project_id" --lease "$profile_environment_id" \
  --session session-compose-smoke-claude --request-id compose-smoke-claude-session-create \
  --idempotency-key compose-smoke-claude-session-create session create --provider claudeAgent)
case "$claude_session_output" in
  *'"providerKind":"claudeAgent"'*'"environmentLeaseId":"'"$profile_environment_id"'"'*'"environmentProfileId":"'"$profile_id"'"'*'"environmentProfileVersion":1'*) ;;
  *) echo "Compose User token did not create the Profile-bound Claude Code Session" >&2; exit 1 ;;
esac
claude_turn_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke-claude \
  --turn turn-compose-smoke-claude --request-id compose-smoke-claude-turn-create \
  --idempotency-key compose-smoke-claude-turn-create turn create --input "verify packaged Compose Runtime")
case "$claude_turn_output" in
  *'"state":"queued"'*) ;;
  *) echo "Compose User token did not persist the Claude Code Turn" >&2; exit 1 ;;
esac
if [ -z "$real_provider_credentials_directory" ]; then
  set +e
  execute_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
    --turn turn-compose-smoke --execution execution-compose-smoke \
    --request-id compose-smoke-execution --idempotency-key compose-smoke-execution \
    execution execute --runtime-mode approval-required --interaction-mode default \
    --input "verify packaged Compose Runtime" 2>&1)
  execute_status=$?
  set -e
  if [ "$execute_status" -ne 2 ] || [ "$execute_output" != "cloud-agentsctl: managedAgentExecute: RUNTIME_FAILED" ]; then
    echo "Compose Runtime failure boundary changed: exit=$execute_status output=$execute_output" >&2
    exit 1
  fi
  execution_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
    --turn turn-compose-smoke --execution execution-compose-smoke \
    --request-id compose-smoke-execution-get execution get)
  case "$execution_output" in
    *'"state":"failed"'*'"errorCode":"runtime_open_failed"'*) ;;
    *) echo "Compose Runtime terminal failure was not persisted: $execution_output" >&2; exit 1 ;;
  esac
  events_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
    --execution execution-compose-smoke --request-id compose-smoke-events \
    events watch --limit 1 --until-terminal)
  case "$events_output" in
    *'"kind":"Event"'*'"operation":"execution.fail"'*'"executionId":"execution-compose-smoke"'*) ;;
    *) echo "Compose event watch did not reach the durable execution terminal event" >&2; exit 1 ;;
  esac
fi

compose restart control-plane >/dev/null
wait_ready
restarted_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-restarted-lease environment-lease get)
case "$restarted_lease_output" in
  *'"generation":3'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"docker-compose-target"'*) ;;
  *) echo "Compose Control Plane restart lost the Profile environment Lease: $restarted_lease_output" >&2; exit 1 ;;
esac
restarted_user_environment_file="$smoke_directory/restarted-user-environment.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$profile_environment_id" \
  compose-smoke-restarted-user-environment >"$restarted_user_environment_file"
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$restarted_user_environment_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
const expectedKeys = ["apiVersion", "environmentId", "expiresAt", "kind", "observedPhase", "profileId", "profileVersion", "projectRef"].sort();
if (value.environmentId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    value.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID || value.profileVersion !== 1 ||
    value.observedPhase !== "ready" || Object.keys(value).sort().join("\n") !== expectedKeys.join("\n")) {
  throw new Error("Control Plane restart changed the safe User Environment projection");
}
NODE
if [ -z "$real_provider_credentials_directory" ]; then
  restarted_execution_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
    --turn turn-compose-smoke --execution execution-compose-smoke \
    --request-id compose-smoke-restarted-execution execution get)
  case "$restarted_execution_output" in
    *'"state":"failed"'*'"errorCode":"runtime_open_failed"'*) ;;
    *) echo "Compose Control Plane restart lost the durable execution" >&2; exit 1 ;;
  esac
fi

execute_real_provider_with_safe_retry() {
  provider_kind=$1
  retry_session_id=$2
  retry_turn_id=$3
  base_execution_id=$4
  request_prefix=$5
  retry_prompt=$6
  execution_file=$7
  attempt=1
  while [ "$attempt" -le 3 ]; do
    attempt_turn_id=$retry_turn_id
    attempt_execution_id=$base_execution_id
    attempt_request_suffix=
    if [ "$attempt" -gt 1 ]; then
      attempt_turn_id="$retry_turn_id-retry-$attempt"
      attempt_execution_id="$base_execution_id-retry-$attempt"
      attempt_request_suffix="-retry-$attempt"
      cloud_agentsctl_user --project "$project_id" --session "$retry_session_id" --turn "$attempt_turn_id" \
        --request-id "$request_prefix$attempt_request_suffix-turn" \
        --idempotency-key "$request_prefix$attempt_request_suffix-turn" \
        turn create --input "$retry_prompt" >/dev/null || return 1
    fi
    if cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$retry_session_id" --turn "$attempt_turn_id" \
      --execution "$attempt_execution_id" --request-id "$request_prefix$attempt_request_suffix" \
      --idempotency-key "$request_prefix$attempt_request_suffix" execution execute \
      --runtime-mode full-access --interaction-mode default --input "$retry_prompt" >"$execution_file"; then
      completed_real_provider_turn_id=$attempt_turn_id
      completed_real_provider_execution_id=$attempt_execution_id
      return 0
    fi

    failure_file="$execution_file.failure-$attempt"
    cloud_agentsctl_user --project "$project_id" --session "$retry_session_id" --turn "$attempt_turn_id" \
      --execution "$attempt_execution_id" --request-id "$request_prefix$attempt_request_suffix-failed" \
      execution get >"$failure_file" || return 1
    if [ "$attempt" -ge 3 ] || ! CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$failure_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
const safeError = ["provider_rate_limited", "provider_unavailable"].includes(value.spec?.errorCode);
const safeMessages = value.messages?.every((message) =>
  message.messageType === "Error" ||
  (message.messageType === "Event" && message.payload?.eventType === "runtime.warning"),
);
if (value.spec?.state !== "failed" || !safeError || value.spec?.checkpoint?.pendingSideEffect === true || !safeMessages) {
  process.exit(1);
}
NODE
    then
      cat "$failure_file" >&2
      return 1
    fi
    echo "Compose real $provider_kind execution $attempt_execution_id failed before Provider side effects; retrying with a new Execution" >&2
    attempt=$((attempt + 1))
    sleep "$attempt"
  done
  return 1
}

run_real_provider_turn() {
  provider_kind=$1
  provider_slug=$2
  session_id="session-$real_provider_run_prefix-$provider_slug"
  turn_id="turn-$real_provider_run_prefix-$provider_slug"
  execution_id="execution-$real_provider_run_prefix-$provider_slug"
  artifact_path=".cloud-agents-stage3-acceptance/$real_provider_environment_slug-target-real-$provider_slug.txt"
  expected_content="cloud-agents $real_provider_environment_label target $provider_kind real E2E"
  case "$provider_kind" in
    codex) file_tool="You must use the workspace.write_text_file tool, never a shell command or another tool, to create" ;;
    claudeAgent) file_tool="You must use the Write tool, never a shell command or another tool, to create" ;;
    pi | deepseek-harness) file_tool="You must use your file-writing tool, never a shell command, to create" ;;
  esac
  prompt="$file_tool exactly one file at $artifact_path. Its complete contents must be the single ASCII line '$expected_content' followed by a newline. Do not modify any other file. Then reply done."

  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-session" --idempotency-key "$real_provider_run_prefix-$provider_slug-session" \
    session create --provider "$provider_kind" --workspace "$real_provider_workspace_id" \
    --sandbox "$real_provider_sandbox_id" --sandbox-generation "$real_provider_sandbox_generation" \
    --environment-profile "$real_provider_environment_profile_id" --environment-profile-version 1 >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-turn" --idempotency-key "$real_provider_run_prefix-$provider_slug-turn" \
    turn create --input "$prompt" >/dev/null
  execution_file="$smoke_directory/$execution_id.json"
  if ! execute_real_provider_with_safe_retry "$provider_kind" "$session_id" "$turn_id" "$execution_id" \
    "$real_provider_run_prefix-$provider_slug-execution" "$prompt" "$execution_file"; then
    return 1
  fi
  turn_id=$completed_real_provider_turn_id
  execution_id=$completed_real_provider_execution_id

  artifact_index=$(
    CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$execution_file" \
    CLOUD_AGENTS_COMPOSE_ARTIFACT_PATH="$artifact_path" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || !value.messages?.some((message) => message.messageType === "Result")) {
  throw new Error("real Provider execution did not succeed with a Result");
}

const indexes = value.messages.flatMap((message, index) => {
  const artifact = message.payload?.artifact;
  return message.messageType === "ArtifactCandidate" &&
    artifact?.sourceRoot === "workspace" && artifact?.path === process.env.CLOUD_AGENTS_COMPOSE_ARTIFACT_PATH &&
    typeof artifact?.kind === "string" && artifact.kind.replaceAll("_", "-") === "generated-file" ? [index] : [];
});
if (indexes.length !== 1) {
  console.error(JSON.stringify(value.messages.map((message) => ({
    messageType: message.messageType,
    eventType: message.payload?.eventType,
    toolName: message.payload?.toolName,
    artifactKind: message.payload?.artifact?.kind,
  }))));
  throw new Error("real Provider execution did not emit the expected generated-file ArtifactCandidate");
}
process.stdout.write(String(indexes[0]));
NODE
  )
  artifact_file="$smoke_directory/$provider_slug-artifact.txt"
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$real_provider_run_prefix-$provider_slug-artifact" \
    execution download-artifact --message-index "$artifact_index" >"$artifact_file"
  CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE="$artifact_file" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT="$expected_content" node <<'NODE'
const { readFileSync } = require("node:fs");
const actual = readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE);
const expected = Buffer.from(`${process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT}\n`);
if (!actual.equals(expected)) throw new Error("real Provider generated-file Artifact content changed");
NODE
  real_events_output=$(cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$session_id" \
    --execution "$execution_id" --request-id "$real_provider_run_prefix-$provider_slug-events" \
    events watch --limit 64 --until-terminal)
  case "$real_events_output" in
    *'"operation":"execution.complete"'*'"executionId":"'"$execution_id"'"'*) ;;
    *) echo "Compose real $provider_kind event watch did not reach execution.complete" >&2; exit 1 ;;
  esac

  assert_real_event_stream_resume "$provider_kind" "$provider_slug" "$session_id" "$execution_id"

  followup_turn_id="$turn_id-followup"
  followup_execution_id="$execution_id-followup"
  followup_prompt="Read $artifact_path and reply with its exact single line. Do not modify any file."
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$followup_turn_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-followup-turn" --idempotency-key "$real_provider_run_prefix-$provider_slug-followup-turn" \
    turn create --input "$followup_prompt" >/dev/null
  followup_file="$smoke_directory/$followup_execution_id.json"
  if ! execute_real_provider_with_safe_retry "$provider_kind" "$session_id" "$followup_turn_id" "$followup_execution_id" \
    "$real_provider_run_prefix-$provider_slug-followup-execution" "$followup_prompt" "$followup_file"; then
    return 1
  fi
  followup_turn_id=$completed_real_provider_turn_id
  followup_execution_id=$completed_real_provider_execution_id
  CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$followup_file" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT="$expected_content" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || !JSON.stringify(value.messages).includes(process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT)) {
  throw new Error("real Provider follow-up Turn did not recover persistent Workspace content");
}
NODE
}

assert_real_event_stream_resume() {
  provider_kind=$1
  provider_slug=$2
  session_id=$3
  execution_id=$4
  events_prefix="$real_provider_run_prefix-$provider_slug"
  first_page_file="$smoke_directory/$provider_slug-events-first-page.json"
  baseline_file="$smoke_directory/$provider_slug-events-baseline.json"
  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$events_prefix-events-first-page" events list --limit 1 >"$first_page_file"
  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$events_prefix-events-baseline" events list --limit 64 >"$baseline_file"
  resume_cursor=$(CLOUD_AGENTS_COMPOSE_EVENTS_FILE="$first_page_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_FILE, "utf8"));
if (!Array.isArray(value.events) || value.events.length !== 1 || typeof value.nextCursor !== "string" || value.nextCursor.length === 0) {
  throw new Error("real Provider event first page must expose a continuation cursor");
}
process.stdout.write(value.nextCursor);
NODE
  )

  interrupted_file="$smoke_directory/$provider_slug-events-interrupted.ndjson"
  interrupted_error_file="$smoke_directory/$provider_slug-events-interrupted.err"
  set +e
  "$cli" --endpoint "https://$endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/user-token" --tenant tenant-compose-smoke --timeout 30s \
    --project "$project_id" --session "$session_id" \
    --request-id "$events_prefix-events-interrupted" events watch --cursor "$resume_cursor" \
    --limit 1 --poll-interval 1s >"$interrupted_file" 2>"$interrupted_error_file" &
  watch_pid=$!
  set -e
  interrupted_ready=0
  attempt=0
  while [ "$attempt" -lt 20 ]; do
    if [ -s "$interrupted_file" ]; then
      interrupted_ready=1
      break
    fi
    if ! kill -0 "$watch_pid" 2>/dev/null; then
      break
    fi
    attempt=$((attempt + 1))
    sleep 0.25
  done
  if [ "$interrupted_ready" -ne 1 ]; then
    kill "$watch_pid" >/dev/null 2>&1 || true
    wait "$watch_pid" >/dev/null 2>&1 || true
    cat "$interrupted_error_file" >&2 || true
    echo "Compose real $provider_kind event watch did not emit before disconnect" >&2
    exit 1
  fi
  kill -TERM "$watch_pid" >/dev/null 2>&1 || true
  set +e
  wait "$watch_pid"
  interrupted_status=$?
  set -e
  if [ "$interrupted_status" -eq 0 ]; then
    echo "Compose real $provider_kind event watch did not observe the forced disconnect" >&2
    exit 1
  fi

  resumed_file="$smoke_directory/$provider_slug-events-resumed.ndjson"
  cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$session_id" \
    --execution "$execution_id" --request-id "$events_prefix-events-resumed" \
    events watch --cursor "$resume_cursor" --limit 64 --until-terminal >"$resumed_file"
  CLOUD_AGENTS_COMPOSE_EVENTS_BASELINE_FILE="$baseline_file" \
  CLOUD_AGENTS_COMPOSE_EVENTS_INTERRUPTED_FILE="$interrupted_file" \
  CLOUD_AGENTS_COMPOSE_EVENTS_RESUMED_FILE="$resumed_file" \
  CLOUD_AGENTS_COMPOSE_EXECUTION_ID="$execution_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const readJSON = (path) => JSON.parse(readFileSync(path, "utf8"));
const readEvents = (path) => readFileSync(path, "utf8").trim().split(/\n+/).filter(Boolean).map((line) => JSON.parse(line));
const baseline = readJSON(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_BASELINE_FILE);
const interrupted = readEvents(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_INTERRUPTED_FILE);
const resumed = readEvents(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_RESUMED_FILE);
if (!Array.isArray(baseline.events) || baseline.events.length < 2 || baseline.hasMore !== false) throw new Error("real Provider event baseline must fit one page");
const expected = baseline.events.slice(1).map((event) => event.metadata.uid);
const actual = resumed.map((event) => event.metadata.uid);
if (interrupted.length === 0 || interrupted[0].metadata.uid !== expected[0]) throw new Error("event watch disconnect did not start at the persisted cursor");
if (actual.length !== expected.length || actual.some((uid, index) => uid !== expected[index])) throw new Error("event watch resume skipped or duplicated persisted events");
const last = resumed.at(-1);
if (last?.spec?.operation !== "execution.complete" || last.spec.executionId !== process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_ID) throw new Error("event watch resume did not reach execution.complete");
process.stdout.write(`event_stream_resume=passed interrupted=${interrupted[0].metadata.uid} resumed_events=${resumed.length}\n`);
NODE
}

run_selected_real_providers() {
  for provider_kind in $real_provider_kinds; do
    case "$provider_kind" in
      codex) provider_slug=codex ;;
      claudeAgent) provider_slug=claude ;;
      pi) provider_slug=pi ;;
      deepseek-harness) provider_slug=deepseek-harness ;;
    esac
    run_real_provider_turn "$provider_kind" "$provider_slug"
  done
}

move_codex_recovery_to_destination() {
  source_sandbox_id=$foundation_agent_sandbox_id
  source_workspace_id=$foundation_agent_workspace_id
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
    compose-recovery-source-before-stop >"$smoke_directory/recovery-source-before-stop.json"
  source_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/recovery-source-before-stop.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)')
  source_stop_generation=${source_stop_values%%|*}
  source_stop_resource_version=${source_stop_values#*|}
  source_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$source_stop_generation" "$source_stop_resource_version" "$source_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_agent_admin_path:stop" \
    compose-recovery-source-stop --header "Idempotency-Key: compose-recovery-source-stop" \
    --data "$source_stop_body" >"$smoke_directory/recovery-source-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
      compose-recovery-source-stopped >"$smoke_directory/recovery-source-stopped.json"
    source_stopped_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/recovery-source-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));if(value.spec?.observedState!=="stopped"||value.spec?.writerReleased!==true)process.exit(1);process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)' 2>/dev/null || true)
    if [ -n "$source_stopped_values" ]; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "Codex recovery source Sandbox did not fence its writer" >&2
      exit 1
    fi
    sleep 1
  done
  source_stopped_generation=${source_stopped_values%%|*}
  recovery_snapshot_id=compose-agent-recovery-snapshot
  recovery_snapshot_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots/$recovery_snapshot_id"
  recovery_snapshot_body=$(printf '{"snapshotId":"%s","sourceSandboxId":"%s","expectedSandboxGeneration":%s,"retentionSeconds":3600}' \
    "$recovery_snapshot_id" "$source_sandbox_id" "$source_stopped_generation")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots" \
    compose-recovery-snapshot compose-recovery-snapshot "$recovery_snapshot_body" \
    "$smoke_directory/recovery-snapshot-created.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$recovery_snapshot_path" \
      compose-recovery-snapshot-get >"$smoke_directory/recovery-snapshot.json"
    recovery_snapshot_values=$(CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE="$smoke_directory/recovery-snapshot.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE,"utf8"));if(value.spec?.status==="failed")throw new Error(value.spec?.stableErrorCode??"snapshot failed");if(value.spec?.status!=="available"||value.spec?.backend!=="portable-tar-v1")process.exit(1);process.stdout.write(`${value.metadata.resourceVersion}|${value.spec.sizeBytes}`)' 2>/dev/null || true)
    if [ -n "$recovery_snapshot_values" ]; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "Codex recovery portable snapshot did not become available" >&2
      exit 1
    fi
    sleep 1
  done
  recovery_snapshot_resource_version=${recovery_snapshot_values%%|*}
  recovery_snapshot_size=${recovery_snapshot_values#*|}
  snapshot_version_negative_body=$(printf '{"expectedSnapshotResourceVersion":"%s","workspaceId":"snapshot-version-negative-workspace","workspaceName":"snapshot-version-negative-workspace","sandboxId":"snapshot-version-negative-sandbox","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":1800}' \
    "$recovery_snapshot_resource_version" "$foundation_agent_restore_profile_id")
  expect_snapshot_backend_rejected "$recovery_snapshot_id" "$recovery_snapshot_resource_version" \
    "$recovery_snapshot_path:restore" "$snapshot_version_negative_body" \
    "$smoke_directory/recovery-snapshot-version-negative.json"
  source_volume=$(docker volume ls -q \
    --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
    --filter label=cloud-agents.dev/project="$project_id" \
    --filter label=cloud-agents.dev/target=docker-compose-target \
    --filter label=cloud-agents.dev/workspace="$source_workspace_id")
  case "$source_volume" in
    '' | *' '*) echo "Codex recovery source Workspace volume inventory changed" >&2; exit 1 ;;
  esac
  docker volume rm "$source_volume" >/dev/null
  docker rm -f "$opensandbox_container" >/dev/null 2>&1 || true
  cross_failover_started_ms=$(node -e 'process.stdout.write(String(Date.now()))')
  if curl --silent --show-error --fail "http://127.0.0.1:$opensandbox_port/health" >/dev/null 2>&1; then
    echo "Codex recovery source execution node remained available" >&2
    exit 1
  fi
  foundation_agent_workspace_id=compose-agent-workspace-restored
  foundation_agent_sandbox_id=compose-agent-sandbox-restored
  foundation_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$foundation_agent_sandbox_id"
  recovery_restore_body=$(printf '{"expectedSnapshotResourceVersion":"%s","workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":1800}' \
    "$recovery_snapshot_resource_version" "$foundation_agent_workspace_id" "$foundation_agent_workspace_id" \
    "$foundation_agent_sandbox_id" "$foundation_agent_restore_profile_id")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" "$recovery_snapshot_path:restore" \
    compose-recovery-snapshot-restore compose-recovery-snapshot-restore "$recovery_restore_body" \
    "$smoke_directory/recovery-sandbox-restoring.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
      compose-recovery-sandbox-restored >"$smoke_directory/recovery-sandbox-restored.json"
    foundation_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/recovery-sandbox-restored.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
    foundation_agent_state=${foundation_agent_values%%|*}
    foundation_agent_rest=${foundation_agent_values#*|}
    foundation_agent_target=${foundation_agent_rest%%|*}
    foundation_agent_rest=${foundation_agent_rest#*|}
    foundation_agent_writer_released=${foundation_agent_rest%%|*}
    foundation_agent_rest=${foundation_agent_rest#*|}
    foundation_agent_generation=${foundation_agent_rest%%|*}
    foundation_agent_rest=${foundation_agent_rest#*|}
    foundation_agent_resource_version=${foundation_agent_rest%%|*}
    foundation_agent_error=${foundation_agent_rest#*|}
    if [ "$foundation_agent_state" = running ] && [ "$foundation_agent_target" = docker-compose-target-restore ] &&
      [ "$foundation_agent_writer_released" = false ]; then
      break
    fi
    if [ "$foundation_agent_state" = failed ]; then
      echo "Codex recovery destination Sandbox failed: $foundation_agent_error" >&2
      exit 1
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Codex recovery destination Sandbox did not become ready: state=$foundation_agent_state target=$foundation_agent_target writerReleased=$foundation_agent_writer_released error=$foundation_agent_error" >&2
      exit 1
    fi
    sleep 1
  done
  cross_recovery_rto_ms=$(CLOUD_AGENTS_COMPOSE_STARTED_MS="$cross_failover_started_ms" node -e \
    'process.stdout.write(String(Date.now()-Number(process.env.CLOUD_AGENTS_COMPOSE_STARTED_MS)))')
  control_plane_api "$smoke_directory/user-curl.conf" GET \
    "/v1/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id" \
    compose-recovery-session-rebound >"$smoke_directory/recovery-session-rebound.json"
  recovery_session=$(cat "$smoke_directory/recovery-session-rebound.json")
  recovery_session_cli=$(cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id compose-recovery-session-rebound-cli session get)
  CLOUD_AGENTS_COMPOSE_SESSION="$recovery_session" CLOUD_AGENTS_COMPOSE_SESSION_CLI="$recovery_session_cli" \
  CLOUD_AGENTS_COMPOSE_WORKSPACE="$foundation_agent_workspace_id" \
  CLOUD_AGENTS_COMPOSE_SANDBOX="$foundation_agent_sandbox_id" \
  CLOUD_AGENTS_COMPOSE_GENERATION="$foundation_agent_generation" node -e \
    'for(const input of [process.env.CLOUD_AGENTS_COMPOSE_SESSION,process.env.CLOUD_AGENTS_COMPOSE_SESSION_CLI]){const parsed=JSON.parse(input);const value=parsed.value??parsed;if(value.spec?.workspaceId!==process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE||value.spec?.sandboxId!==process.env.CLOUD_AGENTS_COMPOSE_SANDBOX||value.spec?.sandboxGeneration!==Number(process.env.CLOUD_AGENTS_COMPOSE_GENERATION))throw new Error(`restored Session binding changed: ${JSON.stringify(parsed)}`)}'
}

move_kubernetes_recovery_to_destination() {
  source_sandbox_id=$kubernetes_agent_sandbox_id
  source_workspace_id=$kubernetes_agent_workspace_id
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$kubernetes_agent_admin_path" \
    compose-kubernetes-recovery-source-before-stop >"$smoke_directory/kubernetes-recovery-source-before-stop.json"
  source_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-recovery-source-before-stop.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)')
  source_stop_generation=${source_stop_values%%|*}
  source_stop_resource_version=${source_stop_values#*|}
  source_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$source_stop_generation" "$source_stop_resource_version" "$source_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$kubernetes_agent_admin_path:stop" \
    compose-kubernetes-recovery-source-stop --header "Idempotency-Key: compose-kubernetes-recovery-source-stop" \
    --data "$source_stop_body" >"$smoke_directory/kubernetes-recovery-source-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$kubernetes_agent_admin_path" \
      compose-kubernetes-recovery-source-stopped >"$smoke_directory/kubernetes-recovery-source-stopped.json"
    source_stopped_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-recovery-source-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));if(value.spec?.observedState!=="stopped"||value.spec?.writerReleased!==true)process.exit(1);process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)' 2>/dev/null || true)
    if [ -n "$source_stopped_values" ]; then break; fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Kubernetes recovery source Sandbox did not fence its writer" >&2
      exit 1
    fi
    sleep 1
  done
  source_stopped_generation=${source_stopped_values%%|*}
  recovery_snapshot_id=compose-kubernetes-agent-recovery-snapshot
  recovery_snapshot_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots/$recovery_snapshot_id"
  recovery_snapshot_body=$(printf '{"snapshotId":"%s","sourceSandboxId":"%s","expectedSandboxGeneration":%s,"retentionSeconds":3600}' \
    "$recovery_snapshot_id" "$source_sandbox_id" "$source_stopped_generation")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots" \
    compose-kubernetes-recovery-snapshot compose-kubernetes-recovery-snapshot "$recovery_snapshot_body" \
    "$smoke_directory/kubernetes-recovery-snapshot-created.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$recovery_snapshot_path" \
      compose-kubernetes-recovery-snapshot-get >"$smoke_directory/kubernetes-recovery-snapshot.json"
    recovery_snapshot_values=$(CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE="$smoke_directory/kubernetes-recovery-snapshot.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE,"utf8"));if(value.spec?.status==="failed")throw new Error(value.spec?.stableErrorCode??"snapshot failed");if(value.spec?.status!=="available"||value.spec?.backend!=="portable-tar-v1")process.exit(1);process.stdout.write(`${value.metadata.resourceVersion}|${value.spec.sizeBytes}`)' 2>/dev/null || true)
    if [ -n "$recovery_snapshot_values" ]; then break; fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Kubernetes recovery portable snapshot did not become available" >&2
      exit 1
    fi
    sleep 1
  done
  recovery_snapshot_resource_version=${recovery_snapshot_values%%|*}
  recovery_snapshot_size=${recovery_snapshot_values#*|}
  cross_failover_started_ms=$(node -e 'process.stdout.write(String(Date.now()))')
  kubernetes_ctl delete namespace "$kubernetes_runtime_namespace" --wait=true --timeout=180s >/dev/null
  docker rm -f "$kubernetes_opensandbox_container" >/dev/null
  kubernetes_node_container="${kind_cluster}-$(printf '%s' "$kubernetes_source_node" | sed "s/^${kind_cluster}-//")"
  if ! docker container inspect "$kubernetes_node_container" >/dev/null 2>&1; then
    kubernetes_node_container=$kubernetes_source_node
  fi
  CLOUD_AGENTS_COMPOSE_KIND_NODE="$kubernetes_node_container" CLOUD_AGENTS_COMPOSE_KIND_CLUSTER="$kind_cluster" node -e \
    'const {execFileSync}=require("node:child_process");const labels=JSON.parse(execFileSync("docker",["inspect",process.env.CLOUD_AGENTS_COMPOSE_KIND_NODE,"--format","{{json .Config.Labels}}"],{encoding:"utf8"}));if(labels["io.x-k8s.kind.cluster"]!==process.env.CLOUD_AGENTS_COMPOSE_KIND_CLUSTER||labels["io.x-k8s.kind.role"]!=="worker")process.exit(1)' || {
      echo "refusing to remove an unverified Kubernetes source node container" >&2
      exit 1
    }
  docker rm -f "$kubernetes_node_container" >/dev/null
  if curl --noproxy '*' --silent --show-error --fail "http://$kubernetes_opensandbox_upstream_host:$kubernetes_opensandbox_port/health" >/dev/null 2>&1; then
    echo "Kubernetes recovery source execution node remained available" >&2
    exit 1
  fi
  kubernetes_agent_workspace_id=compose-kubernetes-agent-workspace-restored
  kubernetes_agent_sandbox_id=compose-kubernetes-agent-sandbox-restored
  kubernetes_active_namespace=$kubernetes_destination_namespace
  kubernetes_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$kubernetes_agent_sandbox_id"
  recovery_restore_body=$(printf '{"expectedSnapshotResourceVersion":"%s","workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":1800}' \
    "$recovery_snapshot_resource_version" "$kubernetes_agent_workspace_id" "$kubernetes_agent_workspace_id" \
    "$kubernetes_agent_sandbox_id" "$kubernetes_agent_restore_profile_id")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" "$recovery_snapshot_path:restore" \
    compose-kubernetes-recovery-snapshot-restore compose-kubernetes-recovery-snapshot-restore "$recovery_restore_body" \
    "$smoke_directory/kubernetes-recovery-sandbox-restoring.json"
  kubernetes_destination_volume_bound=0
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$kubernetes_agent_admin_path" \
      compose-kubernetes-recovery-sandbox-restored >"$smoke_directory/kubernetes-recovery-sandbox-restored.json"
    kubernetes_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-recovery-sandbox-restored.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
    kubernetes_agent_state=${kubernetes_agent_values%%|*}
    kubernetes_agent_rest=${kubernetes_agent_values#*|}
    kubernetes_agent_target=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_writer_released=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_generation=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_resource_version=${kubernetes_agent_rest%%|*}
    kubernetes_agent_error=${kubernetes_agent_rest#*|}
    if [ "$kubernetes_destination_volume_bound" -eq 0 ]; then
      kubernetes_agent_volume=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-recovery-sandbox-restored.json" node -e \
        'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(value.spec?.physicalVolumeId??"")')
      if [ -n "$kubernetes_agent_volume" ]; then
        kubernetes_bind_workspace_volume "$kubernetes_destination_namespace" "$kubernetes_agent_volume" "$kubernetes_destination_node"
        kubernetes_destination_volume_bound=1
      fi
    fi
    if [ "$kubernetes_agent_state" = running ] && [ "$kubernetes_agent_target" = "$kubernetes_destination_target_id" ] &&
      [ "$kubernetes_agent_writer_released" = false ]; then break; fi
    if [ "$kubernetes_agent_state" = failed ]; then
      echo "Kubernetes recovery destination Sandbox failed: $kubernetes_agent_error" >&2
      exit 1
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 240 ]; then
      echo "Kubernetes recovery destination Sandbox did not become ready: state=$kubernetes_agent_state target=$kubernetes_agent_target writerReleased=$kubernetes_agent_writer_released error=$kubernetes_agent_error" >&2
      exit 1
    fi
    sleep 1
  done
  cross_recovery_rto_ms=$(CLOUD_AGENTS_COMPOSE_STARTED_MS="$cross_failover_started_ms" node -e 'process.stdout.write(String(Date.now()-Number(process.env.CLOUD_AGENTS_COMPOSE_STARTED_MS)))')
  control_plane_api "$smoke_directory/user-curl.conf" GET \
    "/v1/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id" \
    compose-kubernetes-recovery-session-rebound >"$smoke_directory/kubernetes-recovery-session-rebound.json"
  recovery_session=$(cat "$smoke_directory/kubernetes-recovery-session-rebound.json")
  recovery_session_cli=$(cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id compose-kubernetes-recovery-session-rebound-cli session get)
  CLOUD_AGENTS_COMPOSE_SESSION="$recovery_session" CLOUD_AGENTS_COMPOSE_SESSION_CLI="$recovery_session_cli" \
  CLOUD_AGENTS_COMPOSE_WORKSPACE="$kubernetes_agent_workspace_id" \
  CLOUD_AGENTS_COMPOSE_SANDBOX="$kubernetes_agent_sandbox_id" \
  CLOUD_AGENTS_COMPOSE_GENERATION="$kubernetes_agent_generation" node -e \
    'for(const input of [process.env.CLOUD_AGENTS_COMPOSE_SESSION,process.env.CLOUD_AGENTS_COMPOSE_SESSION_CLI]){const parsed=JSON.parse(input);const value=parsed.value??parsed;if(value.spec?.workspaceId!==process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE||value.spec?.sandboxId!==process.env.CLOUD_AGENTS_COMPOSE_SANDBOX||value.spec?.sandboxGeneration!==Number(process.env.CLOUD_AGENTS_COMPOSE_GENERATION))throw new Error(`restored Session binding changed: ${JSON.stringify(parsed)}`)}'
}

cleanup_codex_recovery_snapshot() {
  recovery_snapshot_cleanup_body=$(printf '{"expectedSnapshotResourceVersion":"%s","confirmedSnapshotId":"%s","confirmedSourceWorkspaceId":"%s","snapshotDisposition":"delete"}' \
    "$recovery_snapshot_resource_version" "$recovery_snapshot_id" "$source_workspace_id")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" "$recovery_snapshot_path:cleanup" \
    compose-recovery-snapshot-cleanup compose-recovery-snapshot-cleanup "$recovery_snapshot_cleanup_body" \
    "$smoke_directory/recovery-snapshot-cleanup.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$recovery_snapshot_path" \
      compose-recovery-snapshot-cleaned >"$smoke_directory/recovery-snapshot-cleaned.json"
    if CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE="$smoke_directory/recovery-snapshot-cleaned.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE,"utf8"));process.exit(value.spec?.status==="deleted"?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "Codex recovery portable snapshot did not clean up" >&2
      exit 1
    fi
    sleep 1
  done
}

run_recovery_sandbox_probe() {
  probe_request_suffix=$1
  probe_command=$2
  probe_error_file="$smoke_directory/$recovery_prefix-$probe_request_suffix.err"
  probe_attempt=0
  while :; do
    if probe_output=$(cloud_agentsctl_user --timeout 60s --project "$project_id" --sandbox "$recovery_sandbox_id" \
      --request-id "$recovery_prefix-$probe_request_suffix" sandbox exec --expected-generation "$recovery_sandbox_generation" \
      --command "$probe_command" 2>"$probe_error_file"); then
      printf '%s' "$probe_output"
      return 0
    fi
    if ! grep -q 'foundationExecSandbox: RESOURCE_CONFLICT' "$probe_error_file"; then
      cat "$probe_error_file" >&2
      return 1
    fi
    probe_attempt=$((probe_attempt + 1))
    if [ "$probe_attempt" -ge 120 ]; then
      cat "$probe_error_file" >&2
      return 1
    fi
    sleep 0.5
  done
}

stop_foundation_agent() {
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
    compose-smoke-agent-sandbox-stopped >"$smoke_directory/foundation-agent-sandbox-stopped.json"
  foundation_agent_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-agent-sandbox-stopped.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState??"",value.spec?.writerReleased??"",value.spec?.generation??"",value.metadata?.resourceVersion??"",value.spec?.stableErrorCode??""].join("|"))')
  foundation_agent_state=${foundation_agent_stop_values%%|*}
  foundation_agent_stop_rest=${foundation_agent_stop_values#*|}
  foundation_agent_writer_released=${foundation_agent_stop_rest%%|*}
  foundation_agent_stop_rest=${foundation_agent_stop_rest#*|}
  foundation_agent_generation=${foundation_agent_stop_rest%%|*}
  foundation_agent_stop_rest=${foundation_agent_stop_rest#*|}
  foundation_agent_resource_version=${foundation_agent_stop_rest%%|*}
  foundation_agent_error=${foundation_agent_stop_rest#*|}
  if [ "$foundation_agent_state" = stopped ] && [ "$foundation_agent_writer_released" = true ]; then
    return
  fi
  agent_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$foundation_agent_generation" "$foundation_agent_resource_version" "$foundation_agent_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$foundation_agent_admin_path:stop" \
    compose-smoke-agent-sandbox-stop --header "Idempotency-Key: compose-smoke-agent-sandbox-stop" \
    --data "$agent_stop_body" >"$smoke_directory/foundation-agent-sandbox-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$foundation_agent_admin_path" \
      compose-smoke-agent-sandbox-stopped >"$smoke_directory/foundation-agent-sandbox-stopped.json"
    foundation_agent_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/foundation-agent-sandbox-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState??"",value.spec?.writerReleased??"",value.spec?.generation??"",value.metadata?.resourceVersion??"",value.spec?.stableErrorCode??""].join("|"))')
    foundation_agent_state=${foundation_agent_stop_values%%|*}
    foundation_agent_stop_rest=${foundation_agent_stop_values#*|}
    foundation_agent_writer_released=${foundation_agent_stop_rest%%|*}
    foundation_agent_stop_rest=${foundation_agent_stop_rest#*|}
    foundation_agent_generation=${foundation_agent_stop_rest%%|*}
    foundation_agent_stop_rest=${foundation_agent_stop_rest#*|}
    foundation_agent_resource_version=${foundation_agent_stop_rest%%|*}
    foundation_agent_error=${foundation_agent_stop_rest#*|}
    if [ "$foundation_agent_state" = stopped ] && [ "$foundation_agent_writer_released" = true ]; then
      return
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "Compose Agent Sandbox did not stop: state=$foundation_agent_state writerReleased=$foundation_agent_writer_released error=$foundation_agent_error" >&2
      exit 1
    fi
    sleep 1
  done
}

move_remote_worker_recovery_to_destination() {
  source_sandbox_id=$remote_agent_sandbox_id
  source_workspace_id=$remote_agent_workspace_id
  control_plane_api "$smoke_directory/admin-curl.conf" GET "$remote_agent_admin_path" \
    compose-remote-recovery-source-before-stop >"$smoke_directory/remote-recovery-source-before-stop.json"
  source_stop_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-recovery-source-before-stop.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)')
  source_stop_generation=${source_stop_values%%|*}
  source_stop_resource_version=${source_stop_values#*|}
  source_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$source_stop_generation" "$source_stop_resource_version" "$source_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$remote_agent_admin_path:stop" \
    compose-remote-recovery-source-stop --header "Idempotency-Key: compose-remote-recovery-source-stop" \
    --data "$source_stop_body" >"$smoke_directory/remote-recovery-source-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$remote_agent_admin_path" \
      compose-remote-recovery-source-stopped >"$smoke_directory/remote-recovery-source-stopped.json"
    source_stopped_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-recovery-source-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));if(value.spec?.observedState!=="stopped"||value.spec?.writerReleased!==true)process.exit(1);process.stdout.write(`${value.spec.generation}|${value.metadata.resourceVersion}`)' 2>/dev/null || true)
    if [ -n "$source_stopped_values" ]; then break; fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then echo "RemoteWorker recovery source Sandbox did not fence its writer" >&2; exit 1; fi
    sleep 1
  done
  source_stopped_generation=${source_stopped_values%%|*}
  recovery_snapshot_id=compose-remote-agent-recovery-snapshot
  recovery_snapshot_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots/$recovery_snapshot_id"
  recovery_snapshot_body=$(printf '{"snapshotId":"%s","sourceSandboxId":"%s","expectedSandboxGeneration":%s,"retentionSeconds":3600}' \
    "$recovery_snapshot_id" "$source_sandbox_id" "$source_stopped_generation")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workspace-snapshots" \
    compose-remote-recovery-snapshot compose-remote-recovery-snapshot "$recovery_snapshot_body" \
    "$smoke_directory/remote-recovery-snapshot-created.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$recovery_snapshot_path" \
      compose-remote-recovery-snapshot-get >"$smoke_directory/remote-recovery-snapshot.json"
    recovery_snapshot_values=$(CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE="$smoke_directory/remote-recovery-snapshot.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_FILE,"utf8"));if(value.spec?.status==="failed")throw new Error(value.spec?.stableErrorCode??"snapshot failed");if(value.spec?.status!=="available"||value.spec?.backend!=="portable-tar-v1")process.exit(1);process.stdout.write(`${value.metadata.resourceVersion}|${value.spec.sizeBytes}`)' 2>/dev/null || true)
    if [ -n "$recovery_snapshot_values" ]; then break; fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then echo "RemoteWorker recovery portable snapshot did not become available" >&2; exit 1; fi
    sleep 1
  done
  recovery_snapshot_resource_version=${recovery_snapshot_values%%|*}
  recovery_snapshot_size=${recovery_snapshot_values#*|}
  snapshot_version_negative_body=$(printf '{"expectedSnapshotResourceVersion":"%s","workspaceId":"snapshot-version-negative-workspace","workspaceName":"snapshot-version-negative-workspace","sandboxId":"snapshot-version-negative-sandbox","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":1800}' \
    "$recovery_snapshot_resource_version" "$remote_agent_restore_profile_id")
  expect_snapshot_backend_rejected "$recovery_snapshot_id" "$recovery_snapshot_resource_version" \
    "$recovery_snapshot_path:restore" "$snapshot_version_negative_body" \
    "$smoke_directory/remote-recovery-snapshot-version-negative.json"
  stop_foundation_agent
  source_volume=$(docker volume ls -q --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
    --filter label=cloud-agents.dev/project="$project_id" --filter label=cloud-agents.dev/target="$remote_target_id" \
    --filter label=cloud-agents.dev/workspace="$source_workspace_id")
  case "$source_volume" in '' | *' '*) echo "RemoteWorker recovery source Workspace volume inventory changed" >&2; exit 1 ;; esac
  docker volume rm "$source_volume" >/dev/null
  docker rm -f "$opensandbox_container" >/dev/null 2>&1 || true
  docker rm -f "$remote_worker_container" >/dev/null 2>&1 || true
  cross_failover_started_ms=$(node -e 'process.stdout.write(String(Date.now()))')
  if curl --silent --show-error --fail "http://127.0.0.1:$opensandbox_port/health" >/dev/null 2>&1; then echo "RemoteWorker recovery source execution node remained available" >&2; exit 1; fi
  remote_agent_workspace_id=compose-remote-agent-workspace-restored
  remote_agent_sandbox_id=compose-remote-agent-sandbox-restored
  remote_agent_admin_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/sandbox-sessions/$remote_agent_sandbox_id"
  recovery_restore_body=$(printf '{"expectedSnapshotResourceVersion":"%s","workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"%s","runtimeProfileVersion":1,"ttlSeconds":1800}' \
    "$recovery_snapshot_resource_version" "$remote_agent_workspace_id" "$remote_agent_workspace_id" \
    "$remote_agent_sandbox_id" "$remote_agent_restore_profile_id")
  submit_foundation_operation_api "$smoke_directory/admin-curl.conf" "$recovery_snapshot_path:restore" \
    compose-remote-recovery-snapshot-restore compose-remote-recovery-snapshot-restore "$recovery_restore_body" \
    "$smoke_directory/remote-recovery-sandbox-restoring.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$remote_agent_admin_path" \
      compose-remote-recovery-sandbox-restored >"$smoke_directory/remote-recovery-sandbox-restored.json"
    remote_agent_values=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-recovery-sandbox-restored.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.stableErrorCode??""].join("|"))')
    remote_agent_state=${remote_agent_values%%|*}; remote_agent_rest=${remote_agent_values#*|}
    remote_agent_target=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_writer_released=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_generation=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_resource_version=${remote_agent_rest%%|*}; remote_agent_error=${remote_agent_rest#*|}
    if [ "$remote_agent_state" = running ] && [ "$remote_agent_target" = "$remote_target_restore_id" ] && [ "$remote_agent_writer_released" = false ]; then break; fi
    if [ "$remote_agent_state" = failed ]; then echo "RemoteWorker recovery destination Sandbox failed: $remote_agent_error" >&2; exit 1; fi
    attempt=$((attempt + 1)); if [ "$attempt" -ge 240 ]; then echo "RemoteWorker recovery destination Sandbox did not become ready" >&2; exit 1; fi
    sleep 1
  done
  cross_recovery_rto_ms=$(CLOUD_AGENTS_COMPOSE_STARTED_MS="$cross_failover_started_ms" node -e 'process.stdout.write(String(Date.now()-Number(process.env.CLOUD_AGENTS_COMPOSE_STARTED_MS)))')
  control_plane_api "$smoke_directory/user-curl.conf" GET \
    "/v1/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id" \
    compose-remote-recovery-session-rebound >"$smoke_directory/remote-recovery-session-rebound.json"
  recovery_session=$(cat "$smoke_directory/remote-recovery-session-rebound.json")
  recovery_session_cli=$(cloud_agentsctl_user --project "$project_id" --session "$session_id" --request-id compose-remote-recovery-session-rebound-cli session get)
  CLOUD_AGENTS_COMPOSE_SESSION="$recovery_session" CLOUD_AGENTS_COMPOSE_SESSION_CLI="$recovery_session_cli" \
  CLOUD_AGENTS_COMPOSE_WORKSPACE="$remote_agent_workspace_id" CLOUD_AGENTS_COMPOSE_SANDBOX="$remote_agent_sandbox_id" \
  CLOUD_AGENTS_COMPOSE_GENERATION="$remote_agent_generation" node -e \
    'for(const input of [process.env.CLOUD_AGENTS_COMPOSE_SESSION,process.env.CLOUD_AGENTS_COMPOSE_SESSION_CLI]){const parsed=JSON.parse(input);const value=parsed.value??parsed;if(value.spec?.workspaceId!==process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE||value.spec?.sandboxId!==process.env.CLOUD_AGENTS_COMPOSE_SANDBOX||value.spec?.sandboxGeneration!==Number(process.env.CLOUD_AGENTS_COMPOSE_GENERATION))throw new Error(`restored Session binding changed: ${JSON.stringify(parsed)}`)}'
}

run_real_provider_recovery() {
  recovery_provider_kind=$1
  recovery_provider_slug=$2
  recovery_environment_slug=$3
  recovery_environment_label=$4
  recovery_workspace_id=$5
  recovery_sandbox_id=$6
  recovery_sandbox_generation=$7
  recovery_environment_profile_id=$8
  recovery_source_target=$9
  recovery_cross_node=${10}
  recovery_prefix="compose-recovery-$recovery_environment_slug-$recovery_provider_slug"
  if [ "$recovery_cross_node" -eq 1 ]; then
    recovery_prefix="$recovery_prefix-cross-node"
  fi
  session_id="session-$recovery_prefix"
  turn_id="turn-$recovery_prefix"
  execution_id="execution-$recovery_prefix"
  artifact_path=".cloud-agents-stage3-acceptance/$recovery_environment_slug-recovery-$recovery_provider_slug.txt"
  expected_content="cloud-agents $recovery_environment_label $recovery_provider_kind recovered tool result"
  recovery_artifact_absolute="/workspace/.cloud-agents/managed-agent/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id/workspace/$artifact_path"
  expected_recovery_digest=$(CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT="$expected_content" node -e \
    'const {createHash}=require("node:crypto");process.stdout.write(createHash("sha256").update(`${process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT}\n`).digest("hex"))')
  case "$recovery_provider_kind" in
    codex)
      prompt="Use the workspace.write_text_file tool exactly once with path '$artifact_path' and content '$expected_content\n'. Do not use a shell command. Then reply done. Do not reply done unless the managed tool succeeds."
      ;;
    claudeAgent | pi | deepseek-harness)
      recovery_artifact_directory=${artifact_path%/*}
      recovery_shell_command="mkdir -p '$recovery_artifact_directory' && printf '%s\\n' '$expected_content' > '$artifact_path' && sleep 12"
      prompt="Use the Bash tool exactly once to run this exact command: $recovery_shell_command. Do not use another tool. Then reply done. Do not reply done unless the tool succeeds."
      ;;
  esac
  cross_recovery_rto_ms=0
  recovery_snapshot_size=0

  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$recovery_prefix-session" --idempotency-key "$recovery_prefix-session" \
    session create --provider "$recovery_provider_kind" --workspace "$recovery_workspace_id" \
    --sandbox "$recovery_sandbox_id" --sandbox-generation "$recovery_sandbox_generation" \
    --environment-profile "$recovery_environment_profile_id" --environment-profile-version 1 >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --request-id "$recovery_prefix-turn" --idempotency-key "$recovery_prefix-turn" \
    turn create --input "$prompt" >/dev/null

  recovery_execute_file="$smoke_directory/$recovery_prefix-execute.json"
  recovery_execute_error="$smoke_directory/$recovery_prefix-execute.err"
  recovery_execute_status="$smoke_directory/$recovery_prefix-execute.status"
  (
    set +e
    cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$session_id" --turn "$turn_id" \
      --execution "$execution_id" --request-id "$recovery_prefix-execution" \
      --idempotency-key "$recovery_prefix-execution" execution execute \
      --runtime-mode full-access --interaction-mode default --input "$prompt" \
      >"$recovery_execute_file" 2>"$recovery_execute_error"
    printf '%s\n' "$?" >"$recovery_execute_status"
  ) &
  recovery_execute_pid=$!

  recovery_checkpoint_file="$smoke_directory/$recovery_prefix-checkpoint.json"
  attempt=0
  while :; do
    if [ -f "$recovery_execute_status" ]; then
      cat "$recovery_execute_file" "$recovery_execute_error" >&2
      echo "$recovery_provider_kind $recovery_environment_label recovery Turn ended before a pending-side-effect checkpoint" >&2
      exit 1
    fi
    if cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
      --execution "$execution_id" --request-id "$recovery_prefix-checkpoint" \
      execution get >"$recovery_checkpoint_file" 2>/dev/null &&
      CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file" node -e \
        'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE,"utf8"));process.exit(value.spec?.state==="running"&&value.spec?.checkpoint?.pendingSideEffect===true?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "$recovery_provider_kind $recovery_environment_label recovery Turn did not persist a pending-side-effect checkpoint" >&2
      exit 1
    fi
    sleep 1
  done

  attempt=0
  while :; do
    if [ -f "$recovery_execute_status" ]; then
      cat "$recovery_execute_file" "$recovery_execute_error" >&2
      echo "$recovery_provider_kind $recovery_environment_label recovery Turn ended before the injected fault" >&2
      exit 1
    fi
    recovery_pre_fault_probe=$(run_recovery_sandbox_probe pre-fault-probe \
      "if [ -f '$recovery_artifact_absolute' ]; then sha256sum '$recovery_artifact_absolute' | cut -d' ' -f1; else printf 'absent\\n'; fi")
    recovery_pre_fault_stdout=$(CLOUD_AGENTS_COMPOSE_EXECUTION="$recovery_pre_fault_probe" node -e \
      'const value=JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION);if(value.exitCode!==0)process.exit(1);process.stdout.write(value.stdout.trim())')
    if [ "$recovery_pre_fault_stdout" = "$expected_recovery_digest" ]; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "$recovery_provider_kind $recovery_environment_label recovery tool did not apply its side effect before the injected fault" >&2
      exit 1
    fi
    sleep 0.5
  done
  compose kill -s SIGKILL control-plane >/dev/null
  set +e
  wait "$recovery_execute_pid"
  set -e
  if [ ! -f "$recovery_execute_status" ] || [ "$(cat "$recovery_execute_status")" -eq 0 ]; then
    echo "$recovery_provider_kind $recovery_environment_label recovery client did not observe the injected Control Plane crash" >&2
    exit 1
  fi
  compose up -d control-plane >/dev/null
  wait_ready
  if [ "$recovery_cross_node" -eq 1 ]; then
    if [ "$recovery_environment_slug" = kubernetes ]; then
      move_kubernetes_recovery_to_destination
      recovery_workspace_id=$kubernetes_agent_workspace_id
      recovery_sandbox_id=$kubernetes_agent_sandbox_id
      recovery_sandbox_generation=$kubernetes_agent_generation
    elif [ "$recovery_environment_slug" = remote-worker ]; then
      move_remote_worker_recovery_to_destination
      recovery_workspace_id=$remote_agent_workspace_id
      recovery_sandbox_id=$remote_agent_sandbox_id
      recovery_sandbox_generation=$remote_agent_generation
    else
      move_codex_recovery_to_destination
      docker_cross_node_recovery_completed=1
      recovery_workspace_id=$foundation_agent_workspace_id
      recovery_sandbox_id=$foundation_agent_sandbox_id
      recovery_sandbox_generation=$foundation_agent_generation
    fi
  fi

  attempt=0
  while :; do
    cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
      --execution "$execution_id" --request-id "$recovery_prefix-expiry" \
      execution get >"$recovery_checkpoint_file"
    if CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE,"utf8"));process.exit(Date.parse(value.spec?.claimExpiresAt??"")+1000<Date.now()?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "$recovery_provider_kind $recovery_environment_label recovery claim did not expire" >&2
      exit 1
    fi
    sleep 1
  done

  set +e
  recovery_blocked_output=$(cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-execution" \
    --idempotency-key "$recovery_prefix-execution" execution execute \
    --runtime-mode full-access --interaction-mode default --input "$prompt" 2>&1)
  recovery_blocked_status=$?
  set -e
  if [ "$recovery_blocked_status" -eq 0 ]; then
    echo "$recovery_provider_kind $recovery_environment_label recovery replay bypassed side-effect reconciliation" >&2
    exit 1
  fi
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-blocked" \
    execution get >"$recovery_checkpoint_file"
  recovery_values=$(CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE,"utf8"));const checkpoint=value.spec?.checkpoint;if(value.spec?.state!=="running"||value.spec?.recoveryState!=="awaiting_reconciliation"||value.spec?.recoveryReason!=="side_effect_outcome_unknown"||!checkpoint?.digest)process.exit(1);process.stdout.write(`${value.spec.generation}|${checkpoint.digest}`)')
  recovery_generation=${recovery_values%%|*}
  recovery_checkpoint_digest=${recovery_values#*|}

  recovery_probe=$(run_recovery_sandbox_probe probe \
    "if [ -f '$recovery_artifact_absolute' ]; then sha256sum '$recovery_artifact_absolute' | cut -d' ' -f1; else printf 'absent\\n'; fi")
  recovery_probe_stdout=$(CLOUD_AGENTS_COMPOSE_EXECUTION="$recovery_probe" node -e \
    'const value=JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION);if(value.exitCode!==0)process.exit(1);process.stdout.write(value.stdout.trim())')
  case "$recovery_probe_stdout" in
    "$expected_recovery_digest") recovery_outcome=confirmed ;;
    absent) recovery_outcome=not-applied ;;
    *) echo "$recovery_provider_kind $recovery_environment_label recovery side effect has an unexpected result: $recovery_probe_stdout" >&2; exit 1 ;;
  esac
  recovery_reconcile_body=$(printf '{"generation":%s,"checkpointDigest":"%s","outcome":"%s"}' \
    "$recovery_generation" "$recovery_checkpoint_digest" "$recovery_outcome")
  control_plane_api "$smoke_directory/user-curl.conf" POST \
    "/v1/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id/turns/$turn_id/executions/$execution_id:reconcile" \
    "$recovery_prefix-reconcile" --header "Idempotency-Key: $recovery_prefix-reconcile" \
    --data "$recovery_reconcile_body" >/dev/null

  recovery_result_file="$smoke_directory/$recovery_prefix-result.json"
  if ! cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-execution" \
    --idempotency-key "$recovery_prefix-execution" execution execute \
    --runtime-mode full-access --interaction-mode default --input "$prompt" >"$recovery_result_file"; then
    cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
      --execution "$execution_id" --request-id "$recovery_prefix-failure" execution get >&2 || true
    exit 1
  fi
  recovery_expected_mode=process-restart
  recovery_expected_target=$recovery_source_target
  if [ "$recovery_cross_node" -eq 1 ]; then
    recovery_expected_mode=cross-node-takeover
    if [ "$recovery_environment_slug" = kubernetes ]; then
      recovery_expected_target=$kubernetes_destination_target_id
    elif [ "$recovery_environment_slug" = remote-worker ]; then
      recovery_expected_target=$remote_target_restore_id
    else
      recovery_expected_target=docker-compose-target-restore
    fi
  fi
  CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_result_file" \
  CLOUD_AGENTS_COMPOSE_PROVIDER="$recovery_provider_kind" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT="$recovery_environment_slug" \
  CLOUD_AGENTS_COMPOSE_OUTCOME="$recovery_outcome" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_MODE="$recovery_expected_mode" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_SOURCE_TARGET="$recovery_source_target" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_TARGET="$recovery_expected_target" \
  CLOUD_AGENTS_COMPOSE_CROSS_RTO_MS="$cross_recovery_rto_ms" \
  CLOUD_AGENTS_COMPOSE_SNAPSHOT_SIZE="$recovery_snapshot_size" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || value.spec?.attemptNumber !== 2 ||
    value.spec?.recoveryState !== "recovered" || value.spec?.recoveryMode !== process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_MODE ||
    value.spec?.recoveryTargetId !== process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_TARGET ||
    (process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_MODE === "cross-node-takeover" &&
      value.spec?.recoverySourceTargetId !== process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_SOURCE_TARGET) ||
    !value.messages?.some((message) => message.messageType === "Result")) {
  console.error(JSON.stringify(value));
  throw new Error(`${process.env.CLOUD_AGENTS_COMPOSE_PROVIDER} ${process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT} recovery did not safely complete attempt 2`);
}
process.stdout.write(`ANYWHERE_RUNTIME_R4_RECOVERY=${JSON.stringify({
  provider: process.env.CLOUD_AGENTS_COMPOSE_PROVIDER,
  environment: process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT,
  injectedFault: "control-plane-sigkill",
  sideEffectOutcome: process.env.CLOUD_AGENTS_COMPOSE_OUTCOME,
  attemptNumber: value.spec.attemptNumber,
  recoveryState: value.spec.recoveryState,
  recoveryMode: value.spec.recoveryMode,
  recoverySourceTargetId: value.spec.recoverySourceTargetId,
  recoveryTargetId: value.spec.recoveryTargetId,
  checkpoint: value.spec.checkpoint,
  rtoMilliseconds: Number(process.env.CLOUD_AGENTS_COMPOSE_CROSS_RTO_MS),
  rpoBytes: 0,
  snapshotSizeBytes: Number(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_SIZE),
})}\n`);
NODE
  recovery_final_probe=$(run_recovery_sandbox_probe final-probe \
    "sha256sum '$recovery_artifact_absolute' | cut -d' ' -f1")
  CLOUD_AGENTS_COMPOSE_EXECUTION="$recovery_final_probe" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_DIGEST="$expected_recovery_digest" node -e \
    'const value=JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION);if(value.exitCode!==0||value.stdout.trim()!==process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_DIGEST)process.exit(1)'
  if [ "$recovery_cross_node" -eq 1 ]; then
    cleanup_codex_recovery_snapshot
  fi
}

run_selected_real_provider_recoveries() {
  skip_provider=$1
  for provider_kind in $real_provider_kinds; do
    if [ -n "$skip_provider" ] && [ "$provider_kind" = "$skip_provider" ]; then
      continue
    fi
    case "$provider_kind" in
      codex) provider_slug=codex ;;
      claudeAgent) provider_slug=claude ;;
      pi) provider_slug=pi ;;
      deepseek-harness) provider_slug=deepseek-harness ;;
    esac
    run_real_provider_recovery "$provider_kind" "$provider_slug" \
      "$real_provider_environment_slug" "$real_provider_environment_label" \
      "$real_provider_workspace_id" "$real_provider_sandbox_id" "$real_provider_sandbox_generation" \
      "$real_provider_environment_profile_id" "$real_provider_expected_target" 0
  done
}

real_provider_selected() {
  expected_provider=$1
  for provider_kind in $real_provider_kinds; do
    if [ "$provider_kind" = "$expected_provider" ]; then
      return 0
    fi
  done
  return 1
}
if [ -n "$real_provider_credentials_directory" ]; then
  if [ "${CLOUD_AGENTS_COMPOSE_RECOVERY_ONLY:-0}" != 1 ]; then
    real_provider_run_prefix=compose-real
    real_provider_environment_slug=docker
    real_provider_environment_label=Docker
    real_provider_workspace_id=$foundation_agent_workspace_id
    real_provider_sandbox_id=$foundation_agent_sandbox_id
    real_provider_sandbox_generation=$foundation_agent_generation
    real_provider_environment_profile_id=$foundation_agent_environment_profile_id
    run_selected_real_providers
    if [ "$remote_runtime" -eq 1 ]; then
      real_provider_run_prefix=compose-remote-real
      real_provider_environment_slug=remote-worker
      real_provider_environment_label=RemoteWorker
      real_provider_workspace_id=$remote_agent_workspace_id
      real_provider_sandbox_id=$remote_agent_sandbox_id
      real_provider_sandbox_generation=$remote_agent_generation
      real_provider_environment_profile_id=$remote_agent_environment_profile_id
      run_selected_real_providers
    fi
    if [ "$kubernetes_runtime" -eq 1 ]; then
      real_provider_run_prefix=compose-kubernetes-real
      real_provider_environment_slug=kubernetes
      real_provider_environment_label=Kubernetes
      real_provider_workspace_id=$kubernetes_agent_workspace_id
      real_provider_sandbox_id=$kubernetes_agent_sandbox_id
      real_provider_sandbox_generation=$kubernetes_agent_generation
      real_provider_environment_profile_id=$kubernetes_agent_environment_profile_id
      run_selected_real_providers
    fi
  fi
  if [ "$cross_node_environment" = kubernetes ] && [ "$cross_node_recovery" -eq 1 ]; then
    real_provider_environment_slug=kubernetes
    real_provider_environment_label=Kubernetes
    real_provider_workspace_id=$kubernetes_agent_workspace_id
    real_provider_sandbox_id=$kubernetes_agent_sandbox_id
    real_provider_sandbox_generation=$kubernetes_agent_generation
    real_provider_environment_profile_id=$kubernetes_agent_environment_profile_id
    real_provider_expected_target=$kubernetes_runtime_target_id
  elif [ "$cross_node_environment" = remote-worker ] && [ "$cross_node_recovery" -eq 1 ]; then
    real_provider_environment_slug=remote-worker
    real_provider_environment_label=RemoteWorker
    real_provider_workspace_id=$remote_agent_workspace_id
    real_provider_sandbox_id=$remote_agent_sandbox_id
    real_provider_sandbox_generation=$remote_agent_generation
    real_provider_environment_profile_id=$remote_agent_environment_profile_id
    real_provider_expected_target=$remote_target_id
  else
    real_provider_environment_slug=docker
    real_provider_environment_label=Docker
    real_provider_workspace_id=$foundation_agent_workspace_id
    real_provider_sandbox_id=$foundation_agent_sandbox_id
    real_provider_sandbox_generation=$foundation_agent_generation
    real_provider_environment_profile_id=$foundation_agent_environment_profile_id
    real_provider_expected_target=docker-compose-target
  fi
  cross_node_provider_to_run=codex
  if [ "$cross_node_recovery" -eq 1 ]; then
    cross_node_provider_to_run=$cross_node_provider
  fi
  cross_node_recovery_pending=0
  if real_provider_selected "$cross_node_provider_to_run"; then
    case "$cross_node_provider_to_run" in
      codex) cross_node_provider_slug=codex ;;
      claudeAgent) cross_node_provider_slug=claude ;;
      pi) cross_node_provider_slug=pi ;;
      deepseek-harness) cross_node_provider_slug=deepseek-harness ;;
    esac
    if [ "$cross_node_recovery" -eq 1 ]; then
      cross_node_recovery_pending=1
    else
      run_real_provider_recovery "$cross_node_provider_to_run" "$cross_node_provider_slug" docker Docker \
        "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" "$foundation_agent_generation" \
        "$foundation_agent_environment_profile_id" docker-compose-target 0
    fi
  elif [ "$cross_node_recovery" -eq 1 ]; then
      echo "CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER must be selected by CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS" >&2
      exit 2
  fi
  run_agent_interactions() {
    interaction_environment=$1
    interaction_output_directory="$smoke_directory/agent-interactions-$interaction_environment"
    mkdir -m 0700 "$interaction_output_directory"
    if [ "$interaction_environment" = docker ]; then
      CLOUD_AGENTS_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_PROJECT="$project_id" \
      CLOUD_AGENTS_E2E_LEASE_ID="$profile_environment_id" \
      CLOUD_AGENTS_E2E_RUN_ID="compose-agent-interactions-$interaction_environment" \
      CLOUD_AGENTS_E2E_OUTPUT_DIR="$interaction_output_directory" \
      CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER="$(compose ps -q control-plane)" \
      CLOUD_AGENTS_E2E_WORKER_CONTAINER="$(compose ps -q worker)" \
      CLOUD_AGENTS_E2E_POSTGRES_CONTAINER="$(compose ps -q postgres)" \
      CLOUD_AGENTS_E2E_AUTH_CONFIG="$smoke_directory/auth.json" \
      CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY="$smoke_directory/auth-test-private-key.pem" \
      CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID="$foundation_agent_runtime_id" \
      CLOUD_AGENTS_E2E_AGENT_TARGET_ID=docker-compose-target \
      CLOUD_AGENTSCTL="$cli" \
        sh "$script_directory/test-platform-agent-interactions.sh"
    elif [ "$interaction_environment" = remote-worker ]; then
      CLOUD_AGENTS_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_PROJECT="$project_id" \
      CLOUD_AGENTS_E2E_WORKSPACE_ID="$remote_agent_workspace_id" \
      CLOUD_AGENTS_E2E_SANDBOX_ID="$remote_agent_sandbox_id" \
      CLOUD_AGENTS_E2E_SANDBOX_GENERATION="$remote_agent_generation" \
      CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_ID="$remote_agent_environment_profile_id" \
      CLOUD_AGENTS_E2E_RUN_ID="compose-agent-interactions-$interaction_environment" \
      CLOUD_AGENTS_E2E_OUTPUT_DIR="$interaction_output_directory" \
      CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER="$(compose ps -q control-plane)" \
      CLOUD_AGENTS_E2E_WORKER_CONTAINER="$(docker inspect --format '{{.Id}}' "$remote_worker_container")" \
      CLOUD_AGENTS_E2E_REMOTE_WORKER=1 \
      CLOUD_AGENTS_E2E_ADMIN_TOKEN_FILE="$smoke_directory/admin-token" \
      CLOUD_AGENTS_E2E_AGENT_TARGET_ID="$remote_target_id" \
      CLOUD_AGENTS_E2E_POSTGRES_CONTAINER="$(compose ps -q postgres)" \
      CLOUD_AGENTS_E2E_AUTH_CONFIG="$smoke_directory/auth.json" \
      CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY="$smoke_directory/auth-test-private-key.pem" \
      CLOUD_AGENTSCTL="$cli" \
        sh "$script_directory/test-platform-agent-interactions.sh"
    else
      CLOUD_AGENTS_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_PROJECT="$project_id" \
      CLOUD_AGENTS_E2E_WORKSPACE_ID="$kubernetes_agent_workspace_id" \
      CLOUD_AGENTS_E2E_SANDBOX_ID="$kubernetes_agent_sandbox_id" \
      CLOUD_AGENTS_E2E_SANDBOX_GENERATION="$kubernetes_agent_generation" \
      CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_ID="$kubernetes_agent_environment_profile_id" \
      CLOUD_AGENTS_E2E_RUN_ID="compose-agent-interactions-$interaction_environment" \
      CLOUD_AGENTS_E2E_OUTPUT_DIR="$interaction_output_directory" \
      CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER="$(compose ps -q control-plane)" \
      CLOUD_AGENTS_E2E_WORKER_CONTAINER="$(compose ps -q worker)" \
      CLOUD_AGENTS_E2E_POSTGRES_CONTAINER="$(compose ps -q postgres)" \
      CLOUD_AGENTS_E2E_AUTH_CONFIG="$smoke_directory/auth.json" \
      CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY="$smoke_directory/auth-test-private-key.pem" \
      CLOUD_AGENTSCTL="$cli" \
        sh "$script_directory/test-platform-agent-interactions.sh"
    fi
  }
  if [ "${CLOUD_AGENTS_COMPOSE_RECOVERY_ONLY:-0}" != 1 ] &&
    { [ "$sdk_live" -eq 1 ] || { real_provider_selected codex && real_provider_selected claudeAgent; }; }; then
    if real_provider_selected codex && real_provider_selected claudeAgent; then
      run_agent_interactions docker
      wait_ready
      CLOUD_AGENTS_ADMIN_RUNTIME_SMOKE=1 node "$smoke_directory/deployment/scripts/test-platform-compose-admin-web.mjs" \
        "http://$admin_web_endpoint" "$smoke_directory/admin-token" "$smoke_directory/admin-denied-token" \
        "$smoke_directory/user-token" tenant-compose-smoke "$project_id" "http://$user_web_endpoint"
    fi
    if [ "$sdk_live" -eq 1 ]; then
      sdk_package_version=$(CLOUD_AGENTS_SDK_MANIFEST="$candidate_directory/platform-release-manifest.json" node -e \
        'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(process.env.CLOUD_AGENTS_SDK_MANIFEST,"utf8"));if(typeof value.version!=="string")throw new Error("candidate version missing");process.stdout.write(value.version)')
      CLOUD_AGENTS_SDK_PACKAGE_VERSION="$sdk_package_version" \
      CLOUD_AGENTS_SDK_TYPESCRIPT_ARTIFACT="$candidate_directory/cloud-agents-typescript-sdk.tgz" \
      CLOUD_AGENTS_SDK_GO_ARTIFACT="$candidate_directory/cloud-agents-go-sdk.tar" \
      CLOUD_AGENTS_SDK_LIVE_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_SDK_LIVE_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_SDK_LIVE_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_SDK_LIVE_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_SDK_LIVE_PROJECT="$project_id" \
      CLOUD_AGENTS_SDK_LIVE_WORKSPACE="$foundation_agent_workspace_id" \
      CLOUD_AGENTS_SDK_LIVE_SANDBOX="$foundation_agent_sandbox_id" \
      CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION="$foundation_agent_generation" \
      CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE="$foundation_agent_environment_profile_id" \
      CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION=1 \
        bun "$script_directory/test-platform-sdk-consumers.ts"
    fi
  fi
  if [ "$remote_runtime" -eq 1 ]; then
    real_provider_environment_slug=remote-worker
    real_provider_environment_label=RemoteWorker
    real_provider_workspace_id=$remote_agent_workspace_id
    real_provider_sandbox_id=$remote_agent_sandbox_id
    real_provider_sandbox_generation=$remote_agent_generation
    real_provider_environment_profile_id=$remote_agent_environment_profile_id
    real_provider_expected_target=$remote_target_id
    run_selected_real_provider_recoveries ''
    if [ "${CLOUD_AGENTS_COMPOSE_RECOVERY_ONLY:-0}" != 1 ] && real_provider_selected codex && real_provider_selected claudeAgent; then
      run_agent_interactions remote-worker
    fi
  fi
  if [ "$kubernetes_runtime" -eq 1 ]; then
    real_provider_environment_slug=kubernetes
    real_provider_environment_label=Kubernetes
    real_provider_workspace_id=$kubernetes_agent_workspace_id
    real_provider_sandbox_id=$kubernetes_agent_sandbox_id
    real_provider_sandbox_generation=$kubernetes_agent_generation
    real_provider_environment_profile_id=$kubernetes_agent_environment_profile_id
    real_provider_expected_target=$kubernetes_runtime_target_id
    run_selected_real_provider_recoveries ''
    if [ "${CLOUD_AGENTS_COMPOSE_RECOVERY_ONLY:-0}" != 1 ] && real_provider_selected codex && real_provider_selected claudeAgent; then
      run_agent_interactions kubernetes
    fi
  fi
fi

if [ "${cross_node_recovery_pending:-0}" -eq 1 ]; then
  wait_ready
  if [ "$cross_node_environment" = kubernetes ]; then
    run_real_provider_recovery "$cross_node_provider_to_run" "$cross_node_provider_slug" kubernetes Kubernetes \
      "$kubernetes_agent_workspace_id" "$kubernetes_agent_sandbox_id" "$kubernetes_agent_generation" \
      "$kubernetes_agent_environment_profile_id" "$kubernetes_runtime_target_id" 1
  elif [ "$cross_node_environment" = remote-worker ]; then
    run_real_provider_recovery "$cross_node_provider_to_run" "$cross_node_provider_slug" remote-worker RemoteWorker \
      "$remote_agent_workspace_id" "$remote_agent_sandbox_id" "$remote_agent_generation" \
      "$remote_agent_environment_profile_id" "$remote_target_id" 1
  else
    run_real_provider_recovery "$cross_node_provider_to_run" "$cross_node_provider_slug" docker Docker \
      "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" "$foundation_agent_generation" \
      "$foundation_agent_environment_profile_id" docker-compose-target 1
  fi
fi

if [ "$kubernetes_runtime" -eq 1 ]; then
  kubernetes_agent_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$kubernetes_agent_generation" "$kubernetes_agent_resource_version" "$kubernetes_agent_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$kubernetes_agent_admin_path:stop" \
    compose-kubernetes-agent-sandbox-stop --header "Idempotency-Key: compose-kubernetes-agent-sandbox-stop" \
    --data "$kubernetes_agent_stop_body" >"$smoke_directory/kubernetes-agent-sandbox-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$kubernetes_agent_admin_path" \
      compose-kubernetes-agent-sandbox-stopped >"$smoke_directory/kubernetes-agent-sandbox-stopped.json"
    if CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-agent-sandbox-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.exit(value.spec?.observedState==="stopped"&&value.spec?.writerReleased===true&&value.spec?.cleanupPhase==="complete"?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Compose Kubernetes Agent Sandbox did not stop" >&2
      exit 1
    fi
    sleep 1
  done
  kubernetes_agent_pvc_count=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pvc \
    -l cloud-agents.dev/resource=foundation-workspace -o json | \
    CLOUD_AGENTS_COMPOSE_WORKSPACE="$kubernetes_agent_workspace_id" node -e \
      'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(String(value.items.filter((item)=>item.metadata?.annotations?.["cloud-agents.dev/workspace"]===process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE).length))')
  test "$kubernetes_agent_pvc_count" -eq 1
fi

if [ "$remote_runtime" -eq 1 ]; then
  remote_agent_stop_body=$(printf '{"expectedGeneration":%s,"expectedResourceVersion":"%s","confirmedSandboxId":"%s","computeDisposition":"delete","workspaceDisposition":"retain"}' \
    "$remote_agent_generation" "$remote_agent_resource_version" "$remote_agent_sandbox_id")
  control_plane_api "$smoke_directory/admin-curl.conf" POST "$remote_agent_admin_path:stop" \
    compose-remote-agent-sandbox-stop --header "Idempotency-Key: compose-remote-agent-sandbox-stop" \
    --data "$remote_agent_stop_body" >"$smoke_directory/remote-agent-sandbox-stop.json"
  attempt=0
  while :; do
    control_plane_api "$smoke_directory/admin-curl.conf" GET "$remote_agent_admin_path" \
      compose-remote-agent-sandbox-stopped >"$smoke_directory/remote-agent-sandbox-stopped.json"
    if CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-agent-sandbox-stopped.json" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.exit(value.spec?.observedState==="stopped"&&value.spec?.writerReleased===true?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Compose outbound RemoteWorker Agent Sandbox did not stop" >&2
      exit 1
    fi
    sleep 1
  done
  remote_agent_volume=$(docker -H "tcp://127.0.0.1:$destination_docker_port" volume ls -q \
    --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
    --filter label=cloud-agents.dev/project="$project_id" \
    --filter label=cloud-agents.dev/workspace="$remote_agent_workspace_id")
  case "$remote_agent_volume" in
    '' | *' '*) echo "Compose outbound RemoteWorker Workspace volume inventory changed" >&2; exit 1 ;;
  esac
  docker -H "tcp://127.0.0.1:$destination_docker_port" volume rm "$remote_agent_volume" >/dev/null
fi

stop_foundation_agent
if [ "$docker_cross_node_recovery_completed" -eq 1 ]; then
  foundation_agent_volume=$(docker -H "tcp://127.0.0.1:$destination_docker_port" volume ls -q \
    --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
    --filter label=cloud-agents.dev/project="$project_id" \
    --filter label=cloud-agents.dev/workspace="$foundation_agent_workspace_id")
else
  foundation_agent_volume=$(docker volume ls -q \
    --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
    --filter label=cloud-agents.dev/project="$project_id" \
    --filter label=cloud-agents.dev/workspace="$foundation_agent_workspace_id")
fi
case "$foundation_agent_volume" in
  '' | *' '*) echo "Compose Agent Workspace volume inventory changed" >&2; exit 1 ;;
esac
if [ "$docker_cross_node_recovery_completed" -eq 1 ]; then
  docker -H "tcp://127.0.0.1:$destination_docker_port" volume rm "$foundation_agent_volume" >/dev/null
else
  docker volume rm "$foundation_agent_volume" >/dev/null
fi

terminate_file="$smoke_directory/user-environment-terminate.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$profile_environment_id:terminate" \
  compose-smoke-lease-terminate --header "Idempotency-Key: compose-smoke-lease-terminate" \
  --data '{"expectedGeneration":3}' >"$terminate_file"
replayed_terminate_file="$smoke_directory/user-environment-terminate-replayed.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$profile_environment_id:terminate" \
  compose-smoke-lease-terminate --header "Idempotency-Key: compose-smoke-lease-terminate" \
  --data '{"expectedGeneration":3}' >"$replayed_terminate_file"
if ! cmp -s "$replayed_terminate_file" "$terminate_file"; then
  echo "Compose Docker target termination was not idempotent" >&2
  exit 1
fi
terminated_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-terminated-lease environment-lease get)
case "$terminated_lease_output" in
  *'"generation":4'*'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Admin Lease projection did not record User environment termination: $terminated_lease_output" >&2; exit 1 ;;
esac
docker_cleanup_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-docker-target-cleanup --idempotency-key compose-smoke-docker-target-cleanup \
  target cleanup --expected-generation 1 --confirm-target-id docker-compose-target)
case "$docker_cleanup_output" in
  *'"generation":1'*'"targetKind":"docker"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Docker target cleanup failed" >&2; exit 1 ;;
esac
if [ "$cross_node_recovery" -eq 1 ]; then
  if [ "$cross_node_environment" = kubernetes ]; then
    destination_cleanup_output=$(cloud_agentsctl --project "$project_id" --target "$kubernetes_destination_target_id" \
      --request-id compose-kubernetes-restore-target-cleanup --idempotency-key compose-kubernetes-restore-target-cleanup \
      target cleanup --expected-generation 1 --confirm-target-id "$kubernetes_destination_target_id")
  else
    destination_cleanup_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target-restore \
      --request-id compose-smoke-restore-target-cleanup --idempotency-key compose-smoke-restore-target-cleanup \
      target cleanup --expected-generation 1 --confirm-target-id docker-compose-target-restore)
  fi
  case "$destination_cleanup_output" in
    *'"generation":1'*'"observedPhase":"ready"'*) ;;
    *) echo "Compose restore target cleanup failed" >&2; exit 1 ;;
  esac
fi
target_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 0 ]; then
  echo "Compose Docker target Worker was not cleaned up" >&2
  exit 1
fi
admin_workers_after_cleanup_file="$smoke_directory/admin-workers-after-cleanup.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200" \
  compose-smoke-admin-workers-after-cleanup >"$admin_workers_after_cleanup_file"
CLOUD_AGENTS_COMPOSE_WORKERS_FILE="$admin_workers_after_cleanup_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_WORKERS_FILE, "utf8"));
if (value.kind !== "WorkerPage" || !Array.isArray(value.workers) || value.workers.length !== 0) {
  throw new Error("Admin API retained a Worker after Lease termination and cleanup");
}
NODE

backup="$smoke_directory/cloud-agents.dump"
compose --profile backup run --rm -T backup >"$backup"
test -s "$backup"
docker run --rm -i postgres:17.6-bookworm pg_restore --list <"$backup" >/dev/null
compose down --volumes --remove-orphans >/dev/null
compose --profile bootstrap run --rm bootstrap >/dev/null
compose --profile restore run --rm -T restore <"$backup" >/dev/null
set +e
restore_output=$(compose --profile restore run --rm -T restore <"$backup" 2>&1)
restore_status=$?
set -e
case "$restore_output" in
  *"restore target already contains the cloud_agents schema"*) ;;
  *) echo "Compose repeated restore did not fail closed: exit=$restore_status output=$restore_output" >&2; exit 1 ;;
esac
if [ "$restore_status" -eq 0 ]; then
  echo "Compose repeated restore unexpectedly succeeded" >&2
  exit 1
fi

compose up -d >/dev/null
wait_ready
wait_gateway
wait_user_web
wait_admin_web
if [ -z "$real_provider_credentials_directory" ]; then
  restored_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
    --turn turn-compose-smoke --execution execution-compose-smoke \
    --request-id compose-smoke-restored-execution execution get)
  case "$restored_output" in
    *'"state":"failed"'*'"errorCode":"runtime_open_failed"'*) ;;
    *) echo "Compose restore omitted the durable execution" >&2; exit 1 ;;
  esac
fi

echo "platform Compose smoke passed ($image_platform, $cli_target, profile=$profile_id:v1, environment=$profile_environment_id, user-web=same-origin, admin-web=browser, gateway=files+pty+preview+ssh, docker-workers=0, opensandbox-resources=0)"
