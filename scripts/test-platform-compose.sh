#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-compose.sh PLATFORM_RELEASE_DIRECTORY [REAL_PROVIDER_CREDENTIALS_DIRECTORY]" >&2
  exit 2
fi
real_provider_credentials_directory=
real_provider_test=0
real_provider_kinds=${CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS-"codex claudeAgent pi deepseek-harness"}
[ -n "$real_provider_kinds" ] || { echo "CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS must select at least one Provider" >&2; exit 2; }
for provider in $real_provider_kinds; do
  case "$provider" in
    codex | claudeAgent | pi | deepseek-harness) ;;
    *) echo "CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS contains unsupported Provider $provider" >&2; exit 2 ;;
  esac
done
provider_credential_source() {
  provider=$1
  for candidate in \
    "$real_provider_credentials_directory/tenant-compose-smoke.$provider.json" \
    "$real_provider_credentials_directory/tenant-local.$provider.json"; do
    if [ -f "$candidate" ] && [ ! -L "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  echo "real Provider credentials directory is missing a regular tenant-compose-smoke.$provider.json or tenant-local.$provider.json" >&2
  return 1
}
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
  for provider in $real_provider_kinds; do
    provider_credential_source "$provider" >/dev/null
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
capability_contract_only=${CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY:-0}
case "$capability_contract_only" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY must be 0 or 1" >&2; exit 2 ;;
esac
kubernetes_context=${CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT-}
kubernetes_kubeconfig=${CLOUD_AGENTS_COMPOSE_KUBECONFIG-}
if [ "$kubernetes_runtime" -eq 1 ]; then
  if { [ -z "$real_provider_credentials_directory" ] && [ "$capability_contract_only" -ne 1 ]; } || [ -z "$kubernetes_context" ] ||
    [ -z "$kubernetes_kubeconfig" ] || [ ! -f "$kubernetes_kubeconfig" ] || [ -L "$kubernetes_kubeconfig" ]; then
    echo "Kubernetes Runtime smoke requires an explicit context, a regular kubeconfig, and either real Provider credentials or capability contract-only mode" >&2
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
sdk_live=${CLOUD_AGENTS_COMPOSE_SDK_LIVE:-0}
case "$sdk_live" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_SDK_LIVE must be 0 or 1" >&2; exit 2 ;;
esac
capability_acceptance=${CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE:-0}
case "$capability_acceptance" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE must be 0 or 1" >&2; exit 2 ;;
esac
capability_process_recovery=${CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY:-0}
case "$capability_process_recovery" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY must be 0 or 1" >&2; exit 2 ;;
esac
capability_process_recovery_environment=${CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY_ENVIRONMENT-}
case "$capability_process_recovery_environment" in
  '' | docker | remote-worker | kubernetes) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY_ENVIRONMENT must be docker, remote-worker or kubernetes" >&2; exit 2 ;;
esac
capability_process_recovery_ran=0
capability_transport_recovery=${CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY:-0}
case "$capability_transport_recovery" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY must be 0 or 1" >&2; exit 2 ;;
esac
capability_transport_recovery_environment=${CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY_ENVIRONMENT-}
case "$capability_transport_recovery_environment" in
  '' | docker | remote-worker | kubernetes) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY_ENVIRONMENT must be docker, remote-worker or kubernetes" >&2; exit 2 ;;
esac
capability_process_recovery_faults=${CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY_FAULTS:-both}
case "$capability_process_recovery_faults" in
  worker | agent | both) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY_FAULTS must be worker, agent or both" >&2; exit 2 ;;
esac
capability_bound_recovery=${CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY:-1}
case "$capability_bound_recovery" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY must be 0 or 1" >&2; exit 2 ;;
esac
capability_bound_recovery_environment=${CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_ENVIRONMENT-}
case "$capability_bound_recovery_environment" in
  '' | docker | remote-worker | kubernetes) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_ENVIRONMENT must be docker, remote-worker or kubernetes" >&2; exit 2 ;;
esac
capability_bound_recovery_provider=${CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER:-claudeAgent}
case "$capability_bound_recovery_provider" in
  codex | claudeAgent | pi | deepseek-harness) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER must be codex, claudeAgent, pi or deepseek-harness" >&2; exit 2 ;;
esac
if [ "$capability_process_recovery" -eq 1 ] && [ "$capability_acceptance" -ne 1 ]; then
  echo "capability process recovery requires CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1" >&2
  exit 2
fi
if [ "$capability_transport_recovery" -eq 1 ] && [ "$capability_acceptance" -ne 1 ]; then
  echo "capability transport recovery requires CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1" >&2
  exit 2
fi
capability_negative_test=${CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES:-0}
case "$capability_negative_test" in
  0 | 1) ;;
  *) echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES must be 0 or 1" >&2; exit 2 ;;
esac
capability_catalog_test=$((capability_acceptance || capability_negative_test))
if [ "$capability_contract_only" -eq 1 ] && [ "$capability_negative_test" -ne 1 ]; then
  echo "CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY requires CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1" >&2
  exit 2
fi
if [ "$capability_acceptance" -eq 1 ] && [ -z "$real_provider_credentials_directory" ]; then
  echo "capability acceptance requires real Provider credentials" >&2
  exit 2
fi
if [ "$sdk_live" -eq 1 ] && [ "$real_provider_test" -ne 1 ]; then
  echo "live SDK consumer smoke requires real Provider credentials" >&2
  exit 2
fi
if [ "$remote_runtime" -eq 1 ] && { [ "$cross_node_recovery" -ne 1 ] || { [ -z "$real_provider_credentials_directory" ] && [ "$capability_contract_only" -ne 1 ]; }; }; then
  echo "Remote Runtime smoke requires cross-node recovery and either real Provider credentials or capability contract-only mode" >&2
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
kubernetes_api_host=
kubernetes_no_proxy=${NO_PROXY-${no_proxy-}}
kubernetes_ctl() {
  NO_PROXY="$kubernetes_no_proxy" no_proxy="$kubernetes_no_proxy" \
    kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" "$@"
}
verify_kubernetes_runtime_prerequisites() {
  for resource in crd/batchsandboxes.sandbox.opensandbox.io crd/pools.sandbox.opensandbox.io crd/sandboxsnapshots.sandbox.opensandbox.io clusterrole/opensandbox-manager-role; do
    if ! resource_version=$(kubernetes_ctl get "$resource" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/version}' 2>/dev/null) ||
      [ "$resource_version" != 0.2.0 ]; then
      echo "Kubernetes Runtime smoke requires OpenSandbox 0.2.0 prerequisite: $resource" >&2
      return 2
    fi
  done
}
if [ "$kubernetes_runtime" -eq 1 ]; then
  command -v kubectl >/dev/null 2>&1 || { echo "Kubernetes Runtime smoke requires kubectl" >&2; exit 2; }
  kubernetes_api_host=$(kubectl --kubeconfig "$kubernetes_kubeconfig" --context "$kubernetes_context" \
    config view --raw --minify -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
const server = value.clusters?.[0]?.cluster?.server;
if (typeof server !== "string") process.exit(1);
const endpoint = new URL(server);
if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.pathname !== "/" || endpoint.search || endpoint.hash) process.exit(1);
process.stdout.write(endpoint.hostname);
') || { echo "Kubernetes Runtime smoke requires an HTTPS API server endpoint" >&2; exit 2; }
case ",$kubernetes_no_proxy," in
  *,"$kubernetes_api_host",*) ;;
  *) kubernetes_no_proxy="${kubernetes_no_proxy:+$kubernetes_no_proxy,}$kubernetes_api_host" ;;
esac
  kubernetes_ctl cluster-info >/dev/null
  kubernetes_ctl get --raw /version >/dev/null
  verify_kubernetes_runtime_prerequisites
fi
docker_host=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
case "$docker_host" in
  unix:///*) docker_socket=${docker_host#unix://} ;;
  *) echo "platform Compose Docker target smoke requires a Unix Docker context" >&2; exit 2 ;;
esac
docker_gateway=$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
script_directory=$(CDPATH= cd -- "$(dirname "$0")" && pwd -P)
node "$script_directory/lib/platform-release-verifier.ts" "$candidate_directory"
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
cross_node_evidence_file=${CLOUD_AGENTS_COMPOSE_CROSS_NODE_EVIDENCE_FILE-}
snapshot_archive_output=${CLOUD_AGENTS_COMPOSE_SNAPSHOT_ARCHIVE_OUTPUT-}
environment_file="$smoke_directory/compose.env"
base_environment_file="$smoke_directory/compose.no-agent.env"
compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.yml"
managed_agent_compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.managed-agent.yml"
remote_worker_compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.remote-worker.yml"
base_compose_override_file="$smoke_directory/compose-base-override.yml"
compose_override_file="$smoke_directory/compose-target-override.yml"
docker_proxy_pid=
opensandbox_proxy_pid=
kubernetes_opensandbox_proxy_pid=
kubernetes_destination_opensandbox_proxy_pid=
destination_docker_proxy_pid=
destination_opensandbox_proxy_pid=
kubernetes_api_pid=
registry_container="${project}-registry"
worker_attestation_container=
opensandbox_container="${project}-opensandbox"
destination_node_container="${project}-restore-node"
remote_worker_container="${project}-remote-worker"
remote_worker_destination_container="${project}-remote-worker-restore"
kubernetes_opensandbox_container="${project}-kubernetes-opensandbox"
kubernetes_destination_opensandbox_container="${project}-kubernetes-opensandbox-restore"
opensandbox_server_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb"
mcp_fixture_container=
mcp_fixture_docker_host=
mcp_fixture_pod=
mcp_fixture_pod_container=
mcp_fixture_pid=
mcp_fixture_kubernetes=0
mcp_fixture_bundle=
mcp_fixture_marker=
mcp_fixture_side_effect_file=
capability_transport_recovery_ran=0
approval_execute_pid=
mcp_fixture_docker() {
  if [ -n "$mcp_fixture_docker_host" ]; then
    docker -H "$mcp_fixture_docker_host" "$@"
  else
    docker "$@"
  fi
}
kubernetes_controller_image="sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/controller@sha256:a9a5f73c1785ebd955336ffa313973a35c1a1b662cb7afc4ea82d92021b3532a"
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
foundation_sandbox_runtime_id=
kubernetes_agent_workspace_id=compose-kubernetes-agent-workspace
kubernetes_agent_sandbox_id=compose-kubernetes-agent-sandbox
kubernetes_agent_generation=
kubernetes_recovery_bound_pod=
kubernetes_agent_resource_version=
kubernetes_agent_runtime_id=
kubernetes_agent_environment_profile_id=compose-kubernetes-agent-environment-profile
worker_repository=
worker_release_digest=
worker_upgrade_release_digest=
target_worker_credentials_volume="${project}-target-worker-credentials"
target_provider_credentials_volume="${project}-target-provider-credentials"
target_capability_materialization_volume="${project}-target-capabilities"
retry_provider_credentials_volume="${project}-retry-provider-credentials"
project_id=
profile_environment_id=
capability_mcp_id=
capability_skill_id=
capability_mcp_refs_json=
capability_skill_refs_json=
opensandbox_runtime_ids_file=
compose() {
  if [ "$remote_runtime" -eq 1 ]; then
    docker compose --env-file "$environment_file" -f "$compose_file" -f "$managed_agent_compose_file" -f "$remote_worker_compose_file" -f "$compose_override_file" "$@"
  else
    docker compose --env-file "$environment_file" -f "$compose_file" -f "$managed_agent_compose_file" -f "$compose_override_file" "$@"
  fi
}
compose_base() {
  docker compose --env-file "$base_environment_file" -f "$compose_file" -f "$base_compose_override_file" "$@"
}
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  if [ -n "$approval_execute_pid" ]; then
    kill "$approval_execute_pid" >/dev/null 2>&1 || true
    wait "$approval_execute_pid" 2>/dev/null || true
  fi
  if [ -n "$mcp_fixture_container" ] && [ "$mcp_fixture_kubernetes" -ne 1 ]; then
    if [ "$status" -ne 0 ]; then
      mcp_fixture_docker logs "$mcp_fixture_container" >&2 || true
    fi
    mcp_fixture_docker rm -f "$mcp_fixture_container" >/dev/null 2>&1 || true
  fi
  if [ -n "$mcp_fixture_pod" ] && [ -n "$mcp_fixture_pod_container" ]; then
    if [ -n "$mcp_fixture_pid" ]; then
      kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
        sh -c "kill $mcp_fixture_pid" >/dev/null 2>&1 || true
    fi
    kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'rm -f /tmp/cloud-agents-mcp-fixture.mjs /tmp/cloud-agents-mcp-capabilities.json /tmp/cloud-agents-mcp-fixture.log /tmp/cloud-agents-mcp-side-effect/result.txt /tmp/cloud-agents-mcp-side-effect/requests.log /tmp/cloud-agents-mcp-side-effect/disconnect-arm /tmp/cloud-agents-mcp-side-effect/disconnect-arm.committed; rmdir /tmp/cloud-agents-mcp-side-effect 2>/dev/null || true' \
      >/dev/null 2>&1 || true
  fi
  docker rm -f "$kubernetes_opensandbox_container" >/dev/null 2>&1 || true
  docker rm -f "$kubernetes_destination_opensandbox_container" >/dev/null 2>&1 || true
  if [ "$kubernetes_runtime" -eq 1 ]; then
    for namespace in "$kubernetes_runtime_namespace" "$kubernetes_destination_namespace" "$kubernetes_operator_namespace"; do
      owner=$(kubernetes_ctl get namespace "$namespace" -o json 2>/dev/null |
        node -e 'const fs=require("node:fs");const input=fs.readFileSync(0,"utf8");if(input)process.stdout.write(JSON.parse(input).metadata?.labels?.["cloud-agents.dev/test-run"]??"")' || true)
      if [ "$owner" = "$project" ]; then
        kubernetes_ctl delete namespace "$namespace" --wait=true --timeout=180s >/dev/null 2>&1 || status=1
      elif [ -n "$owner" ]; then
        echo "refusing to delete Kubernetes namespace without the exact test ownership label: $namespace" >&2
        status=1
      fi
    done
    for resource in "clusterrolebinding/$kubernetes_version_binding" "clusterrolebinding/$kubernetes_manager_binding" \
      "clusterrolebinding/$kubernetes_destination_version_binding" "clusterrolebinding/$kubernetes_destination_manager_binding" \
      "clusterrole/$kubernetes_version_role"; do
      owner=$(kubernetes_ctl get "$resource" -o json 2>/dev/null |
        node -e 'const fs=require("node:fs");const input=fs.readFileSync(0,"utf8");if(input)process.stdout.write(JSON.parse(input).metadata?.labels?.["cloud-agents.dev/test-run"]??"")' || true)
      if [ "$owner" = "$project" ]; then
        kubernetes_ctl delete "$resource" --wait=true --timeout=60s >/dev/null 2>&1 || status=1
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
  if [ ! -e "$opensandbox_runtime_ids_file" ]; then
    echo "OpenSandbox runtime ID inventory file was missing; refusing broad cleanup" >&2
    status=1
  elif [ -s "$opensandbox_runtime_ids_file" ]; then
    owned_opensandbox_volumes_file="$smoke_directory/opensandbox-owned-volumes"
    : >"$owned_opensandbox_volumes_file" 2>/dev/null || true
    while IFS= read -r runtime_id; do
      case "$runtime_id" in
        '' | *[!A-Za-z0-9._:-]*)
          echo "refusing to clean OpenSandbox runtime with an invalid owned ID" >&2
          status=1
          continue
          ;;
      esac
      runtime_container_found=0
      managed_volumes=
      runtime_containers=$(docker ps -aq --filter "label=opensandbox.io/id=$runtime_id" || true)
      for container in $runtime_containers; do
        [ -n "$container" ] || continue
        runtime_container_found=1
        if [ "$status" -ne 0 ]; then docker logs "$container" >&2 || true; fi
        container_managed_volumes=$(docker inspect --format '{{index .Config.Labels "opensandbox.io/volume-managed-by"}}' "$container" 2>/dev/null || true)
        if [ -n "$container_managed_volumes" ]; then
          container_managed_volumes=$(CLOUD_AGENTS_COMPOSE_MANAGED_VOLUMES_JSON="$container_managed_volumes" node -e \
            'const raw=process.env.CLOUD_AGENTS_COMPOSE_MANAGED_VOLUMES_JSON??"";let value;try{value=JSON.parse(raw)}catch{process.exit(0)}if(!Array.isArray(value))process.exit(0);for(const name of value)if(typeof name==="string"&&/^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$/.test(name))process.stdout.write(`${name}\n`)' || true)
          managed_volumes="$managed_volumes$container_managed_volumes"
        fi
        docker rm -f "$container" >/dev/null 2>&1 || true
      done
      if [ -z "$managed_volumes" ]; then
        # OpenSandbox's Docker backend deterministically names its managed
        # runtime volume. This remains an exact owned-ID lookup, never a list.
        if [ "$runtime_container_found" -eq 0 ]; then
          echo "OpenSandbox owned runtime container was already absent; checking only its deterministic volume" >&2
        else
          echo "OpenSandbox runtime container exposed no valid managed-volume list; checking only its deterministic volume" >&2
        fi
        managed_volumes="opensandbox-runtime-$runtime_id"
      fi
      for volume in $managed_volumes; do
        case "$volume" in
          '' | *[!A-Za-z0-9_.-]*)
            echo "refusing to inspect an invalid OpenSandbox volume name" >&2
            status=1
            continue
            ;;
        esac
        if [ -f "$owned_opensandbox_volumes_file" ] && grep -Fqx "$volume" "$owned_opensandbox_volumes_file"; then
          continue
        fi
        printf '%s\n' "$volume" >>"$owned_opensandbox_volumes_file"
        volume_owner_label=$(docker volume inspect "$volume" --format '{{index .Labels "opensandbox.io/volume-managed-by"}}' 2>/dev/null || true)
        if [ -z "$volume_owner_label" ]; then
          continue
        fi
        if [ "$volume_owner_label" = server ]; then
          docker volume rm "$volume" >/dev/null 2>&1 || true
        else
          echo "refusing to delete OpenSandbox volume without the server ownership label" >&2
          status=1
        fi
      done
      for container in $(docker ps -aq --filter "label=opensandbox.io/egress-sidecar-for=$runtime_id"); do
        if [ "$status" -ne 0 ]; then docker logs "$container" >&2 || true; fi
        docker rm -f "$container" >/dev/null 2>&1 || true
      done
    done <"$opensandbox_runtime_ids_file"
  else
    echo "No OpenSandbox runtime IDs were recorded; no runtime cleanup required" >&2
  fi
  if [ -n "$project_id" ]; then
    for volume in $(docker volume ls -q \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id"); do
      docker volume rm "$volume" >/dev/null 2>&1 || true
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
  docker rm -f "$worker_attestation_container" >/dev/null 2>&1 || true
  docker rm -f "${project}-worker-upgrade-seed" >/dev/null 2>&1 || true
  docker volume rm "$target_worker_credentials_volume" "$target_provider_credentials_volume" "$target_capability_materialization_volume" \
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
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$smoke_directory/deployment" "$smoke_directory/access-gateway-tls" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" "$smoke_directory/capability-materialization-control-plane" "$smoke_directory/capability-materialization-worker" "$smoke_directory/workspace" "$smoke_directory/snapshots" \
  "$smoke_directory/mcp-side-effect" \
  "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/docker-target-credentials/docker-compose-target-restore" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/prepared-kubernetes-target-credentials" \
  "$smoke_directory/ssh-target-credentials" \
  "$smoke_directory/remote-worker-ca" "$smoke_directory/remote-worker-node" "$smoke_directory/remote-worker-node-restore" \
  "$smoke_directory/fake-kubectl-state" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
opensandbox_runtime_ids_file="$smoke_directory/opensandbox-runtime-ids"
: >"$opensandbox_runtime_ids_file"
remember_opensandbox_runtime_id() {
  remember_runtime_id=$1
  case "$remember_runtime_id" in
    '') return 0 ;;
    *[!A-Za-z0-9._:-]*)
      echo "refusing to record an invalid OpenSandbox runtime ID" >&2
      return 0
      ;;
  esac
  grep -Fqx "$remember_runtime_id" "$opensandbox_runtime_ids_file" 2>/dev/null ||
    printf '%s\n' "$remember_runtime_id" >>"$opensandbox_runtime_ids_file"
}
mcp_fixture_bundle="$smoke_directory/managed-capability-mcp-server.mjs"
docker ps -aq --filter label=opensandbox.io/id | sort >"$smoke_directory/opensandbox-runtime-baseline"
docker ps -aq --filter label=opensandbox.io/egress-sidecar-for | sort >"$smoke_directory/opensandbox-egress-baseline"
docker volume ls -q --filter label=opensandbox.io/volume-managed-by=server | sort >"$smoke_directory/opensandbox-volume-baseline"
chmod 0755 "$smoke_directory" "$smoke_directory/access-gateway-tls" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" "$smoke_directory/capability-materialization-control-plane" "$smoke_directory/capability-materialization-worker" \
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
chmod 0777 "$smoke_directory/mcp-side-effect"
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
CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER="$capability_bound_recovery_provider" \
  node <<'NODE'
const { createSign, generateKeyPairSync, randomBytes } = require("node:crypto");
const { chmodSync, writeFileSync } = require("node:fs");
const { isIP } = require("node:net");

const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const release = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_RELEASE;
const project = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PROJECT;
const platform = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PLATFORM;
const dockerGateway = process.env.CLOUD_AGENTS_COMPOSE_DOCKER_GATEWAY;
const codexWriteReceiptDelay = process.env.CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST === "1" &&
  process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER === "codex" ? "30000" : "12000";
const deepseekHarnessToolDelay = process.env.CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS ?? "";
const deepseekHarnessManagedToolDelay = process.env.CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS ?? "";
if (![state, release, project, platform, dockerGateway].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Compose smoke environment");
}
if (/[\r\n]/u.test(deepseekHarnessToolDelay)) throw new Error("invalid DeepSeek harness delay");
if (/[\r\n]/u.test(deepseekHarnessManagedToolDelay)) throw new Error("invalid DeepSeek managed-tool delay");
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
  "mcp-servers.create", "mcp-servers.get", "mcp-servers.list", "mcp-servers.delete", "skill-bundles.create", "skill-bundles.get", "skill-bundles.list", "skill-bundles.delete",
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
    ? [`CLOUD_AGENT_CODEX_MANAGED_WRITE_RECEIPT_DELAY_MS=${codexWriteReceiptDelay}`]
    : []),
  ...(deepseekHarnessToolDelay ? [`CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS=${deepseekHarnessToolDelay}`] : []),
  ...(deepseekHarnessManagedToolDelay ? [`CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS=${deepseekHarnessManagedToolDelay}`] : []),
  "",
].join("\n"));
writeFileSync(`${state}/provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-worker-credentials/admission-token`, admissionToken);
writeFileSync(`${state}/kubernetes-target-credentials/kubernetes-compose-target.token`, `${kubernetesToken}\n`);
const workerImageOverride = process.env.CLOUD_AGENTS_COMPOSE_WORKER_IMAGE;
if (workerImageOverride && /[\r\n]/u.test(workerImageOverride)) throw new Error("invalid Worker image override");
const workerOverride = workerImageOverride
  ? `\n  worker:\n    image: ${JSON.stringify(workerImageOverride)}\n    build: !reset null\n`
  : "";
const baseComposeOverride = `services:\n  access-gateway:\n    environment:\n      SSL_CERT_FILE: /run/cloud-agents/tls/ca.crt\n    extra_hosts:\n      - "host.docker.internal:host-gateway"\n  control-plane:\n    environment:\n      SSL_CERT_FILE: /run/cloud-agents/tls/ca.crt\n      CLOUD_AGENT_CODEX_MANAGED_WRITE_RECEIPT_DELAY_MS: "${process.env.CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST === "1" ? codexWriteReceiptDelay : ""}"\n      CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS: "${deepseekHarnessToolDelay}"\n      CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS: "${deepseekHarnessManagedToolDelay}"\n    extra_hosts:\n      - "host.docker.internal:host-gateway"\n`;
writeFileSync(`${state}/compose-base-override.yml`, baseComposeOverride);
writeFileSync(`${state}/compose-target-override.yml`, `${baseComposeOverride}${workerOverride}`);
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
  `CLOUD_AGENTS_CONTROL_PLANE_CAPABILITY_MATERIALIZATION_DIR=${state}/capability-materialization-control-plane`,
  `CLOUD_AGENTS_WORKER_CAPABILITY_MATERIALIZATION_DIR=${state}/capability-materialization-worker`,
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
  "CLOUD_AGENTS_RUNTIME_ENV_FILE=", "CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIR=", "CLOUD_AGENTS_CONTROL_PLANE_CAPABILITY_MATERIALIZATION_DIR=", "CLOUD_AGENTS_WORKER_CAPABILITY_MATERIALIZATION_DIR=", "CLOUD_AGENTS_CAPABILITY_MATERIALIZATION_DIR=", "CLOUD_AGENTS_SNAPSHOT_DIR=",
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
no_agent_authority_matches=$(grep -En 'CLOUD_AGENTS_PLATFORM_(WORKER|ADMISSION|CAPABILITY|PROVIDER_CREDENTIALS)|target: /run/cloud-agents/(provider-credentials|capabilities)|target: /var/lib/cloud-agents/snapshots|^[[:space:]]+worker:' "$smoke_directory/no-agent-config.yml" || true)
if [ -n "$no_agent_authority_matches" ]; then
  printf '%s\n' "$no_agent_authority_matches" >&2
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
start_capability_mcp_fixture() {
  mcp_fixture_runtime_id=$1
  previous_mcp_fixture_docker_host=$mcp_fixture_docker_host
  mcp_fixture_preserve_state=${3:-0}
  if [ "$mcp_fixture_kubernetes" -eq 1 ] && [ -n "${mcp_fixture_pid-}" ] &&
    [ -n "${mcp_fixture_pod-}" ] && [ -n "${mcp_fixture_pod_container-}" ]; then
    # Kubernetes Runtime reuses the Pod across checks; stop the prior fixture
    # before binding its fixed loopback port again.
    kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" \
      -c "$mcp_fixture_pod_container" -- sh -c "kill $mcp_fixture_pid 2>/dev/null || true" \
      >/dev/null 2>&1 || true
    mcp_fixture_pid=
  fi
  if [ -n "$mcp_fixture_container" ] && [ "$mcp_fixture_kubernetes" -ne 1 ]; then
    if [ -n "$previous_mcp_fixture_docker_host" ]; then
      docker -H "$previous_mcp_fixture_docker_host" rm -f "$mcp_fixture_container" >/dev/null 2>&1 || true
    else
      docker rm -f "$mcp_fixture_container" >/dev/null 2>&1 || true
    fi
  fi
  mcp_fixture_docker_host=${2-}
  mcp_fixture_kubernetes=0
  if [ "$capability_acceptance" -ne 1 ]; then
    return 0
  fi
  case " $real_provider_kinds " in
    *" codex "* | *" claudeAgent "* | *" pi "* | *" deepseek-harness "*) ;;
    *) return 0 ;;
  esac
  fixture_script="$script_directory/fixtures/managed-capability-mcp-server.mjs"
  fixture_sdk="$script_directory/../node_modules/.bun/@modelcontextprotocol+sdk@1.30.0/node_modules/@modelcontextprotocol/sdk/dist/esm/server/mcp.js"
  if [ ! -f "$fixture_script" ] || [ ! -f "$fixture_sdk" ]; then
    echo "capability acceptance requires the pinned @modelcontextprotocol/sdk fixture" >&2
    return 1
  fi
  if [ "$mcp_fixture_preserve_state" -eq 0 ]; then
    mcp_fixture_marker=$(openssl rand -hex 16)
  elif [ -z "$mcp_fixture_marker" ]; then
    echo "capability MCP fixture cannot preserve state without an existing marker" >&2
    return 1
  fi
  mcp_fixture_container="${project}-mcp-fixture"
  if [ "$mcp_fixture_preserve_state" -eq 0 ]; then
    rm -f "$smoke_directory/mcp-side-effect/result.txt" "$smoke_directory/mcp-side-effect/requests.log" \
      "$smoke_directory/mcp-side-effect/disconnect-arm" "$smoke_directory/mcp-side-effect/disconnect-arm.committed"
  fi
  mcp_fixture_side_effect_file="$smoke_directory/mcp-side-effect/result.txt"
  if [ "$real_provider_environment_slug" = kubernetes ]; then
    command -v bun >/dev/null 2>&1 || { echo "Kubernetes capability acceptance requires bun to bundle the MCP fixture" >&2; return 1; }
    if [ ! -f "$mcp_fixture_bundle" ]; then
      bun build "$fixture_script" --target=node --outfile="$mcp_fixture_bundle" >/dev/null
    fi
    attempt=0
    while :; do
      mcp_fixture_pod=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pods \
        -l "opensandbox.io/id=$mcp_fixture_runtime_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
      case "$mcp_fixture_pod" in
        '' | *' '*)
          attempt=$((attempt + 1))
          if [ "$attempt" -ge 120 ]; then
            echo "capability acceptance could not resolve the Kubernetes Runtime Pod" >&2
            return 1
          fi
          sleep 0.5
          ;;
        *) break ;;
      esac
    done
    mcp_fixture_pod_container=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pod "$mcp_fixture_pod" \
      -o jsonpath='{.spec.containers[0].name}')
    case "$mcp_fixture_pod" in
      '' | *' '*) echo "capability acceptance could not resolve the Kubernetes Runtime Pod" >&2; return 1 ;;
    esac
    case "$mcp_fixture_pod_container" in
      '' | *' '*) echo "capability acceptance could not resolve the Kubernetes Runtime container" >&2; return 1 ;;
    esac
    kubernetes_ctl -n "$kubernetes_active_namespace" exec -i "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'umask 077; cat > /tmp/cloud-agents-mcp-fixture.mjs' <"$mcp_fixture_bundle"
    kubernetes_ctl -n "$kubernetes_active_namespace" exec -i "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'umask 077; cat > /tmp/cloud-agents-mcp-capabilities.json' \
      <"$smoke_directory/capability-materialization-worker/tenant-compose-smoke.capabilities.json"
    if [ "$mcp_fixture_preserve_state" -eq 0 ]; then
      kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
        sh -c 'rm -rf /tmp/cloud-agents-mcp-side-effect; mkdir /tmp/cloud-agents-mcp-side-effect' >/dev/null
    else
      kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
        sh -c 'mkdir -p /tmp/cloud-agents-mcp-side-effect' >/dev/null
    fi
    mcp_fixture_pid=$(kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'rm -f /tmp/cloud-agents-mcp-fixture.log; nohup env MCP_ACCEPTANCE_DESCRIPTOR=/tmp/cloud-agents-mcp-capabilities.json MCP_ACCEPTANCE_SIDE_EFFECT=/tmp/cloud-agents-mcp-side-effect/result.txt MCP_ACCEPTANCE_LOG=/tmp/cloud-agents-mcp-side-effect/requests.log MCP_ACCEPTANCE_MARKER='"$mcp_fixture_marker"' MCP_ACCEPTANCE_DISCONNECT_ARM=/tmp/cloud-agents-mcp-side-effect/disconnect-arm node /tmp/cloud-agents-mcp-fixture.mjs >/tmp/cloud-agents-mcp-fixture.log 2>&1 </dev/null & echo $!')
    mcp_fixture_kubernetes=1
    attempt=0
    until kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'grep -Fqx mcp-acceptance-ready /tmp/cloud-agents-mcp-fixture.log' >/dev/null 2>&1; do
      if ! kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
        sh -c "kill -0 $mcp_fixture_pid" >/dev/null 2>&1; then
        kubernetes_ctl -n "$kubernetes_active_namespace" logs "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" --tail=20 >&2 || true
        kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
          sh -c 'cat /tmp/cloud-agents-mcp-fixture.log' >&2 || true
        echo "capability acceptance Kubernetes MCP fixture did not start" >&2
        return 1
      fi
      attempt=$((attempt + 1))
      if [ "$attempt" -ge 60 ]; then
        echo "capability acceptance Kubernetes MCP fixture did not become ready" >&2
        return 1
      fi
      sleep 0.25
    done
    echo "capability MCP fixture ready pod=$mcp_fixture_pod runtime=$mcp_fixture_runtime_id" >&2
    return 0
  fi
  if [ -n "$mcp_fixture_docker_host" ]; then
    command -v bun >/dev/null 2>&1 || { echo "RemoteWorker capability acceptance requires bun to bundle the MCP fixture" >&2; return 1; }
    if [ ! -f "$mcp_fixture_bundle" ]; then
      bun build "$fixture_script" --target=node --outfile="$mcp_fixture_bundle" >/dev/null
    fi
    if ! mcp_fixture_docker image inspect node:24.18.1-bookworm-slim >/dev/null 2>&1; then
      if ! docker image inspect node:24.18.1-bookworm-slim >/dev/null 2>&1; then
        echo "RemoteWorker capability acceptance requires node:24.18.1-bookworm-slim on the source Docker daemon" >&2
        return 1
      fi
      docker image save node:24.18.1-bookworm-slim | mcp_fixture_docker image load >/dev/null
    fi
    mcp_fixture_source_mount=/dind-config/managed-capability-mcp-server.mjs
    mcp_fixture_source_destination=/fixture.mjs
    mcp_fixture_descriptor_mount=/dind-config/capability-materialization-worker
    mcp_fixture_side_effect_mount=/dind-config/mcp-side-effect
    mcp_fixture_command_path=/fixture.mjs
  else
    mcp_fixture_source_mount=$script_directory/..
    mcp_fixture_source_destination=/source
    mcp_fixture_descriptor_mount=$smoke_directory/capability-materialization-worker
    mcp_fixture_side_effect_mount=$smoke_directory/mcp-side-effect
    mcp_fixture_command_path=/source/scripts/fixtures/managed-capability-mcp-server.mjs
  fi
  if mcp_fixture_docker container inspect "$mcp_fixture_runtime_id" >/dev/null 2>&1; then
    mcp_fixture_runtime_container=$mcp_fixture_runtime_id
  else
    mcp_fixture_runtime_container=$(mcp_fixture_docker ps -q --filter "label=opensandbox.io/id=$mcp_fixture_runtime_id")
  fi
  case "$mcp_fixture_runtime_container" in
    '' | *' '*) echo "capability acceptance could not resolve the Agent Runtime container" >&2; return 1 ;;
  esac
  mcp_fixture_docker run -d --name "$mcp_fixture_container" --network "container:$mcp_fixture_runtime_container" \
    --mount "type=bind,src=$mcp_fixture_source_mount,dst=$mcp_fixture_source_destination,readonly" \
    --mount "type=bind,src=$mcp_fixture_descriptor_mount,dst=/capabilities,readonly" \
    --mount "type=bind,src=$mcp_fixture_side_effect_mount,dst=/side-effect" \
    --user 1000:1000 \
    -e MCP_ACCEPTANCE_DESCRIPTOR=/capabilities/tenant-compose-smoke.capabilities.json \
    -e MCP_ACCEPTANCE_SIDE_EFFECT=/side-effect/result.txt \
    -e MCP_ACCEPTANCE_LOG=/side-effect/requests.log \
    -e MCP_ACCEPTANCE_MARKER="$mcp_fixture_marker" \
    -e MCP_ACCEPTANCE_DISCONNECT_ARM=/side-effect/disconnect-arm \
    node:24.18.1-bookworm-slim node "$mcp_fixture_command_path" >/dev/null
  attempt=0
  until mcp_fixture_docker logs "$mcp_fixture_container" 2>&1 | grep -Fqx "mcp-acceptance-ready"; do
    if ! mcp_fixture_docker inspect --format '{{.State.Running}}' "$mcp_fixture_container" 2>/dev/null | grep -Fxq true; then
      echo "capability acceptance MCP fixture did not start" >&2
      return 1
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "capability acceptance MCP fixture did not become ready" >&2
      return 1
    fi
    sleep 0.25
  done
  echo "capability MCP fixture ready container=$mcp_fixture_container runtime=$mcp_fixture_runtime_container" >&2
}

sync_capability_mcp_fixture() {
  if [ "$mcp_fixture_kubernetes" -ne 1 ]; then
    return 0
  fi
  sync_attempt=1
  sync_error="$smoke_directory/mcp-side-effect/kubernetes-exec-sync.stderr"
  while [ "$sync_attempt" -le 3 ]; do
    sync_pods=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pods \
      -l "opensandbox.io/id=$mcp_fixture_runtime_id" \
      -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.metadata.name}{"\n"}{end}' \
      2>"$sync_error" || true)
    sync_pod=$(printf '%s\n' "$sync_pods" | sed -n '1p')
    sync_pod_container=
    if [ -n "$sync_pod" ]; then
      sync_pod_container=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pod "$sync_pod" \
        -o jsonpath='{.spec.containers[0].name}' 2>>"$sync_error" || true)
    fi
    sync_result_tmp="$smoke_directory/mcp-side-effect/result.txt.sync.$sync_attempt"
    sync_requests_tmp="$smoke_directory/mcp-side-effect/requests.log.sync.$sync_attempt"
    rm -f "$sync_result_tmp" "$sync_requests_tmp"
    if [ -n "$sync_pod" ] && [ -n "$sync_pod_container" ] && \
      kubernetes_ctl -n "$kubernetes_active_namespace" exec -i "$sync_pod" -c "$sync_pod_container" -- \
        sh -c 'cat /tmp/cloud-agents-mcp-side-effect/result.txt 2>/dev/null || true' \
        >"$sync_result_tmp" 2>>"$sync_error" && \
      kubernetes_ctl -n "$kubernetes_active_namespace" exec -i "$sync_pod" -c "$sync_pod_container" -- \
        sh -c 'cat /tmp/cloud-agents-mcp-side-effect/requests.log 2>/dev/null || true' \
        >"$sync_requests_tmp" 2>>"$sync_error"; then
      mv "$sync_result_tmp" "$smoke_directory/mcp-side-effect/result.txt"
      mv "$sync_requests_tmp" "$smoke_directory/mcp-side-effect/requests.log"
      mcp_fixture_pod="$sync_pod"
      mcp_fixture_pod_container="$sync_pod_container"
      return 0
    fi
    rm -f "$sync_result_tmp" "$sync_requests_tmp"
    if [ "$sync_attempt" -lt 3 ]; then
      sleep 0.5
    fi
    sync_attempt=$((sync_attempt + 1))
  done
  echo "Kubernetes MCP fixture synchronization failed after 3 exec attempts" >&2
  tail -n 20 "$sync_error" >&2 2>/dev/null || true
  return 1
}

capability_mcp_tool_call_count() {
  sync_capability_mcp_fixture
  awk '$0 == "rpc tools/call" { count += 1 } END { print count + 0 }' \
    "$smoke_directory/mcp-side-effect/requests.log" 2>/dev/null
}

arm_capability_mcp_disconnect() {
  if [ "$mcp_fixture_kubernetes" -eq 1 ]; then
    kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      sh -c 'umask 077; rm -f /tmp/cloud-agents-mcp-side-effect/disconnect-arm.committed; : > /tmp/cloud-agents-mcp-side-effect/disconnect-arm'
  else
    mcp_fixture_docker exec "$mcp_fixture_container" sh -c \
      'umask 077; rm -f /side-effect/disconnect-arm.committed; : > /side-effect/disconnect-arm; test -f /side-effect/disconnect-arm'
  fi
}

wait_capability_mcp_disconnect_commit() {
  attempt=0
  while :; do
    if [ "$mcp_fixture_kubernetes" -eq 1 ]; then
      if kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
        sh -c 'test -f /tmp/cloud-agents-mcp-side-effect/disconnect-arm.committed && ! kill -0 '"$mcp_fixture_pid"' 2>/dev/null'; then
        break
      fi
    elif [ -f "$smoke_directory/mcp-side-effect/disconnect-arm.committed" ]; then
      mcp_fixture_running=$(mcp_fixture_docker inspect --format '{{.State.Running}}' "$mcp_fixture_container" 2>/dev/null || true)
      if [ "$mcp_fixture_running" = false ]; then
        break
      fi
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 120 ]; then
      echo "capability MCP fixture did not commit the transport fault and stop" >&2
      return 1
    fi
    sleep 0.25
  done
  sync_capability_mcp_fixture
}

assert_capability_mcp_listener_from_runtime_netns() {
  listener_probe='fetch("http://127.0.0.1:48765/mcp").then((response)=>process.exit(response.status===401?0:1),()=>process.exit(1))'
  if [ "$mcp_fixture_kubernetes" -eq 1 ]; then
    kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
      node -e "$listener_probe"
  else
    mcp_fixture_docker exec "$mcp_fixture_container" node -e "$listener_probe"
  fi
}

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
  *'/var/run/docker.sock'* | *'credentials'* | *'capabilities'*) echo "User Web received infrastructure authority" >&2; exit 1 ;;
esac
admin_web_container=$(compose ps -q admin-web)
test "$(docker inspect --format '{{.Config.User}}' "$admin_web_container")" = "1000:1000"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$admin_web_container")" = true
test "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$admin_web_container")" = '["ALL"]'
case "$(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$admin_web_container")" in
  *'/var/run/docker.sock'* | *'credentials'* | *'capabilities'*) echo "Admin Web received infrastructure authority" >&2; exit 1 ;;
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
read_recovery_snapshot_digest() {
  recovery_snapshot_postgres_container=$(compose ps -q postgres)
  printf '%s\n' "SELECT content_digest FROM cloud_agents.workspace_snapshots WHERE tenant_id = :'tenant' AND project_uid = :'project' AND snapshot_uid = :'snapshot';" |
    docker exec -i "$recovery_snapshot_postgres_container" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
      -v tenant=tenant-compose-smoke -v project="$project_id" -v snapshot="$recovery_snapshot_id"
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
worker_attestation_container="${project}-worker-attestation"
docker rm -f "$worker_attestation_container" >/dev/null 2>&1 || true
docker create --name "$worker_attestation_container" "$worker_image" >/dev/null
docker cp "$worker_attestation_container:/usr/local/bin/cloud-agents-worker" "$smoke_directory/worker-attested" >/dev/null
docker cp "$worker_attestation_container:/usr/local/bin/cloud-agent-runtime" "$smoke_directory/runtime-attested" >/dev/null
docker cp "$worker_attestation_container:/usr/local/bin/cloud-agents-landlock-run" "$smoke_directory/landlock-attested" >/dev/null
docker cp "$worker_attestation_container:/usr/share/doc/cloud-agents/landlock-notices.txt" "$smoke_directory/landlock-notices-attested" >/dev/null
candidate_worker="$candidate_directory/cloud-agents-worker-${image_platform%/*}-${image_platform#*/}"
if ! cmp -s "$candidate_worker" "$smoke_directory/worker-attested" ||
  ! cmp -s "$candidate_directory/cloud-agent-runtime-standalone.mjs" "$smoke_directory/runtime-attested" ||
  ! cmp -s "$candidate_directory/cloud-agents-landlock-run-${image_platform%/*}-${image_platform#*/}" "$smoke_directory/landlock-attested" ||
  ! cmp -s "$candidate_directory/cloud-agents-landlock-notices.txt" "$smoke_directory/landlock-notices-attested"; then
  echo "Compose Worker image does not contain the candidate Worker, Runtime and Landlock artifacts and notices" >&2
  exit 1
fi
docker rm "$worker_attestation_container" >/dev/null
worker_attestation_container=
docker tag "$worker_image" "$worker_repository:smoke"
docker push "$worker_repository:smoke" >/dev/null
worker_reference=$(docker image inspect "$worker_repository:smoke" --format '{{json .RepoDigests}}' |
  CLOUD_AGENTS_WORKER_REPOSITORY="$worker_repository" node -e 'const fs=require("node:fs");const repo=process.env.CLOUD_AGENTS_WORKER_REPOSITORY+"@";const values=JSON.parse(fs.readFileSync(0,"utf8")).filter((value)=>value.startsWith(repo+"sha256:"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
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
  CLOUD_AGENTS_WORKER_REPOSITORY="$worker_repository" node -e 'const fs=require("node:fs");const repo=process.env.CLOUD_AGENTS_WORKER_REPOSITORY+"@";const values=JSON.parse(fs.readFileSync(0,"utf8")).filter((value)=>value.startsWith(repo+"sha256:"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
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
  for image in "$opensandbox_server_image" "$kubernetes_controller_image" "$kubernetes_execd_image" "$kubernetes_egress_image"; do
    case "$image" in
      *@sha256:????????????????????????????????????????????????????????????????) ;;
      *) echo "Kubernetes Runtime smoke requires digest-pinned OpenSandbox images" >&2; exit 1 ;;
    esac
  done
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

  for service_account in control-plane opensandbox-server; do
    kubernetes_ctl -n "$kubernetes_runtime_namespace" get serviceaccount "$service_account" >/dev/null
  done
  kubernetes_ctl -n "$kubernetes_runtime_namespace" get rolebinding foundation-workspace >/dev/null
  kubernetes_ctl get clusterrolebinding "$kubernetes_manager_binding" >/dev/null
  kubernetes_ctl auth can-i --as="system:serviceaccount:$kubernetes_runtime_namespace:control-plane" get /version >/dev/null
  kubernetes_ctl auth can-i --as="system:serviceaccount:$kubernetes_runtime_namespace:opensandbox-server" create pods >/dev/null
  kubernetes_ctl auth can-i --as="system:serviceaccount:$kubernetes_runtime_namespace:opensandbox-server" create pods/exec >/dev/null

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
  kubernetes_capability_materialization_field=
  if [ "$capability_acceptance" -eq 1 ]; then
    kubernetes_capability_materialization_field=',"capabilityMaterializationSecretRef":"cloud-agents-capabilities-target"'
  fi
  printf '{"namespace":"%s","workerImageRepository":"%s","workerCredentialSecretRef":"cloud-agents-worker-target"%s,"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target","workerServerName":"worker-target.example"}\n' \
    "$kubernetes_runtime_namespace" "$worker_repository" "$kubernetes_capability_materialization_field" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_runtime_credential_ref.deployment.json"
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
    printf '{"namespace":"%s","workerImageRepository":"%s","workerCredentialSecretRef":"cloud-agents-worker-target"%s,"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target","workerServerName":"worker-target.example"}\n' \
      "$kubernetes_destination_namespace" "$worker_repository" "$kubernetes_capability_materialization_field" >"$smoke_directory/kubernetes-target-credentials/$kubernetes_destination_credential_ref.deployment.json"
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

if [ "$capability_acceptance" -eq 1 ]; then
  capability_descriptor_source="$real_provider_credentials_directory/tenant-compose-smoke.capabilities.json"
  if [ ! -f "$capability_descriptor_source" ] || [ -L "$capability_descriptor_source" ]; then
    echo "capability acceptance requires tenant-compose-smoke.capabilities.json" >&2
    exit 2
  fi
  docker run --rm --user 0 --entrypoint /bin/sh \
    -v "$smoke_directory/capability-materialization-control-plane:/target-control-plane" \
    -v "$smoke_directory/capability-materialization-worker:/target-worker" \
    -v "$real_provider_credentials_directory:/source:ro" \
    postgres:17.6-bookworm -ec \
    'cp /source/tenant-compose-smoke.capabilities.json /target-control-plane/; cp /source/tenant-compose-smoke.capabilities.json /target-worker/; for key in /source/*.pub; do [ -f "$key" ] || continue; cp "$key" /target-control-plane/; cp "$key" /target-worker/; done; chown 65532:65532 /target-control-plane/*; chown 1000:1000 /target-worker/*; chmod 0400 /target-control-plane/* /target-worker/*'
fi

CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY="$worker_repository" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
CLOUD_AGENTS_COMPOSE_SMOKE_CAPABILITY_REF="$([ "$capability_acceptance" -eq 1 ] && printf '%s' "$target_capability_materialization_volume")" \
  node <<'NODE'
const { chmodSync, writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const workerImageRepository = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY;
const workerCredentialRef = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF;
const capabilityMaterializationRef = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_CAPABILITY_REF;
if (![state, workerImageRepository, workerCredentialRef].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Docker target deployment inputs");
}
const descriptor = {
  workerImageRepository,
  workerCredentialRef,
  workerSpiffeId: "spiffe://cloud-agents.compose/worker-target",
  workerServerName: "host.docker.internal",
};
if (capabilityMaterializationRef) descriptor.capabilityMaterializationRef = capabilityMaterializationRef;
const path = `${state}/docker-target-credentials/docker-compose-target/deployment.json`;
writeFileSync(path, `${JSON.stringify(descriptor)}\n`);
chmodSync(path, 0o444);
const kubernetesDescriptorPath = `${state}/kubernetes-target-credentials/kubernetes-compose-target.deployment.json`;
const kubernetesDescriptor = { namespace: "cloud-agents-target", workerImageRepository, workerCredentialSecretRef: "cloud-agents-worker-target", workerSpiffeId: "spiffe://cloud-agents.compose/worker-target", workerServerName: "worker-target.example" };
if (capabilityMaterializationRef) kubernetesDescriptor.capabilityMaterializationSecretRef = "cloud-agents-capabilities-target";
writeFileSync(kubernetesDescriptorPath, `${JSON.stringify(kubernetesDescriptor)}\n`);
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
  CLOUD_AGENTS_CAPABILITY_MATERIALIZATION_SECRET_REF="$([ "$capability_acceptance" -eq 1 ] && printf '%s' cloud-agents-capabilities-target)" \
  CLOUD_AGENTS_CAPABILITY_MATERIALIZATION_DIR="$([ "$capability_acceptance" -eq 1 ] && printf '%s' "$smoke_directory/capability-materialization-worker")" \
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
CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE="$capability_acceptance" node <<'NODE'
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
const capabilityAcceptance = process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE === "1";
if (descriptor.namespace !== "cloud-agents-target" || descriptor.workerCredentialSecretRef !== "cloud-agents-worker-target" ||
    (capabilityAcceptance ? descriptor.capabilityMaterializationSecretRef !== "cloud-agents-capabilities-target" : "capabilityMaterializationSecretRef" in descriptor)) throw new Error("prepared Kubernetes descriptor changed");
const rbac = readFileSync(`${state}/fake-kubectl-state/rbac.yaml`, "utf8");
const expectedSecrets = capabilityAcceptance ?
  'resourceNames: ["cloud-agents-worker-target", "cloud-agents-provider-target", "cloud-agents-capabilities-target"]' :
  'resourceNames: ["cloud-agents-worker-target", "cloud-agents-provider-target"]';
if (!rbac.includes(expectedSecrets) || !rbac.includes('resources: ["pods/exec"]') || !rbac.includes('verbs: ["get", "create"]') || !rbac.includes('verbs: ["get", "list", "create", "patch", "delete"]')) throw new Error("prepared Kubernetes RBAC changed");
if (readFileSync(`${state}/fake-kubectl-state/calls`, "utf8").includes("fake-service-account-token")) throw new Error("Kubernetes token entered command log");
NODE

prepare_target_credentials() {
  CLOUD_AGENTS_WORKER_IMAGE="$worker_reference" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_DIR="$smoke_directory/target-worker-credentials" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF="$target_provider_credentials_volume" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR="$smoke_directory/target-provider-credentials" \
  CLOUD_AGENTS_CAPABILITY_MATERIALIZATION_REF="$([ "$capability_acceptance" -eq 1 ] && printf '%s' "$target_capability_materialization_volume")" \
  CLOUD_AGENTS_CAPABILITY_MATERIALIZATION_DIR="$([ "$capability_acceptance" -eq 1 ] && printf '%s' "$smoke_directory/capability-materialization-worker")" \
  CLOUD_AGENTS_TENANT=tenant-compose-smoke \
    sh "$smoke_directory/deployment/scripts/prepare-platform-docker-target.sh"
}
prepare_target_credentials >/dev/null
if prepare_target_credentials >/dev/null 2>&1; then
  echo "Docker target preparation overwrote non-empty credential volumes" >&2
  exit 1
fi
if [ -n "$real_provider_credentials_directory" ]; then
  for provider in $real_provider_kinds; do
    credential_source=$(provider_credential_source "$provider")
    credential_target="$smoke_directory/provider-credentials/tenant-compose-smoke.$provider.json"
    cp "$credential_source" "$credential_target"
    chmod 0444 "$credential_target"
  done
  for provider in $real_provider_kinds; do
    docker run --rm --user 0 --entrypoint /bin/sh \
      -v "$target_provider_credentials_volume:/target" \
      -v "$smoke_directory/provider-credentials:/source:ro" \
      postgres:17.6-bookworm -ec \
      "cp /source/tenant-compose-smoke.$provider.json /target/ && chown 1000:1000 /target/tenant-compose-smoke.$provider.json && chmod 0400 /target/tenant-compose-smoke.$provider.json"
  done
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
    --env "CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS=${CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS:-}" \
    --env "CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS=${CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS:-}" \
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
      --env "CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS=${CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS:-}" \
      --env "CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS=${CLOUD_AGENT_DEEPSEEK_HARNESS_MANAGED_TOOL_DELAY_MS:-}" \
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
capability_session_flags=
capability_execution_flags=
capability_bound_recoveries=
if [ "$capability_catalog_test" -eq 1 ]; then
  if [ "$capability_acceptance" -eq 1 ]; then
    capability_descriptor="$smoke_directory/capability-materialization-worker/tenant-compose-smoke.capabilities.json"
    capability_values=$(CAPABILITY_DESCRIPTOR="$capability_descriptor" node <<'NODE'
const { readFileSync } = require("node:fs");
const descriptor = JSON.parse(readFileSync(process.env.CAPABILITY_DESCRIPTOR, "utf8"));
if (descriptor.version !== 1 || descriptor.mcp?.length !== 1 || descriptor.skills?.length !== 1) throw new Error("capability acceptance requires exactly one MCP and one Skill");
const mcp = descriptor.mcp[0];
const skill = descriptor.skills[0];
for (const value of [mcp.resourceId, mcp.version, mcp.digest, mcp.transport, skill.resourceId, skill.version, skill.digest]) {
  if (typeof value !== "string" || /[\s\n]/u.test(value)) throw new Error("capability descriptor contains an invalid identity");
}
process.stdout.write(JSON.stringify({mcp, skill}));
NODE
    )
  else
    capability_values='{"mcp":{"resourceId":"mcp-compose-negative","version":"1.0.0","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","transport":"streamable-http"},"skill":{"resourceId":"skill-compose-negative","version":"1.0.0","digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}'
  fi
  capability_mcp_id=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).mcp.resourceId)')
  capability_mcp_version=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).mcp.version)')
  capability_mcp_digest=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).mcp.digest)')
  capability_mcp_transport=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).mcp.transport)')
  capability_skill_id=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).skill.resourceId)')
  capability_skill_version=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).skill.version)')
  capability_skill_digest=$(CAPABILITY_VALUES="$capability_values" node -e 'process.stdout.write(JSON.parse(process.env.CAPABILITY_VALUES).skill.digest)')
  capability_mcp_body=$(printf '{"serverId":"%s","version":"%s","digest":"%s","transport":"%s","connectionRef":"connection-compose-acceptance","credentialRef":"credential-compose-acceptance","networkPolicyRef":"network-compose","permissions":["tools.call"]}' \
    "$capability_mcp_id" "$capability_mcp_version" "$capability_mcp_digest" "$capability_mcp_transport")
  capability_skill_body=$(printf '{"bundleId":"%s","version":"%s","digest":"%s","sourceRef":"source-compose-acceptance","signatureRef":"signature-compose-acceptance","signingKeyId":"key-1","compatibleProviders":["codex","claude-code","pi","deepseek-harness"]}' \
    "$capability_skill_id" "$capability_skill_version" "$capability_skill_digest")
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/mcp-servers" \
    compose-smoke-capability-mcp-create --header "Idempotency-Key: compose-smoke-capability-mcp-create" \
    --data "$capability_mcp_body" >"$smoke_directory/capability-mcp.json"
  control_plane_api "$smoke_directory/admin-curl.conf" POST \
    "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/skill-bundles" \
    compose-smoke-capability-skill-create --header "Idempotency-Key: compose-smoke-capability-skill-create" \
    --data "$capability_skill_body" >"$smoke_directory/capability-skill.json"
  CAPABILITY_MCP_FILE="$smoke_directory/capability-mcp.json" CAPABILITY_SKILL_FILE="$smoke_directory/capability-skill.json" node <<'NODE'
const { readFileSync } = require("node:fs");
const mcp = JSON.parse(readFileSync(process.env.CAPABILITY_MCP_FILE, "utf8"));
const skill = JSON.parse(readFileSync(process.env.CAPABILITY_SKILL_FILE, "utf8"));
if (mcp.kind !== "McpServer" || mcp.spec?.status !== "active" || skill.kind !== "SkillBundle" || skill.spec?.status !== "active" || skill.spec?.mountReadOnly !== true) throw new Error("capability catalog did not create active resources");
NODE
  capability_mcp_refs_json=$(printf '[{"serverId":"%s","version":"%s","digest":"%s"}]' "$capability_mcp_id" "$capability_mcp_version" "$capability_mcp_digest")
  capability_skill_refs_json=$(printf '[{"bundleId":"%s","version":"%s","digest":"%s"}]' "$capability_skill_id" "$capability_skill_version" "$capability_skill_digest")
  capability_session_flags="--mcp-server-refs-json $capability_mcp_refs_json --skill-bundle-refs-json $capability_skill_refs_json"
  capability_execution_flags="$capability_session_flags"
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
worker_profile_provider_kinds=$(CLOUD_AGENTS_PROVIDER_KINDS="$real_provider_kinds" node <<'NODE'
const providers = new Set(["codex", "claudeAgent"]);
for (const provider of process.env.CLOUD_AGENTS_PROVIDER_KINDS.split(/\s+/u).filter(Boolean)) providers.add(provider);
process.stdout.write(JSON.stringify([...providers]));
NODE
)
profile_create_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Docker worker profile","providerKinds":%s,"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"network-compose","releaseDigest":"%s","targetRefs":["%s"],"providerCredentialRef":"%s"}' \
  "$profile_id" "$profile_id" "$worker_profile_provider_kinds" "$worker_release_digest" docker-compose-target "$target_provider_credentials_volume")
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
  throw new Error(`Profile did not create a safe ready User Environment: ${actualKeys.join(",")}${value.stableErrorCode ? ` stableErrorCode=${value.stableErrorCode}` : ""}`);
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
  throw new Error(`Admin API did not close the Worker upgrade operation: ${JSON.stringify(value)}`);
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
  foundation_agent_allowed_egress=$(CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIRECTORY="$smoke_directory/provider-credentials" CLOUD_AGENTS_PROVIDER_KINDS="$real_provider_kinds" node <<'NODE'
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const directory = process.env.CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIRECTORY;
const providers = process.env.CLOUD_AGENTS_PROVIDER_KINDS.split(/\s+/u).filter(Boolean);
const targets = new Set();
for (const [provider, fallback] of providers.map((provider) => [provider, { codex: "api.openai.com", claudeAgent: "api.anthropic.com", pi: "api.openai.com", "deepseek-harness": "api.deepseek.com" }[provider]])) {
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
foundation_release_digest="${CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST:-sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5}"
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
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
  foundation_sandbox_state=${foundation_sandbox_values%%|*}
  foundation_sandbox_rest=${foundation_sandbox_values#*|}
  foundation_sandbox_generation=${foundation_sandbox_rest%%|*}
  foundation_sandbox_rest=${foundation_sandbox_rest#*|}
  foundation_sandbox_resource_version=${foundation_sandbox_rest%%|*}
  foundation_sandbox_rest=${foundation_sandbox_rest#*|}
  foundation_sandbox_runtime_id=${foundation_sandbox_rest%%|*}
  remember_opensandbox_runtime_id "$foundation_sandbox_runtime_id"
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
  remember_opensandbox_runtime_id "$foundation_agent_runtime_id"
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
run_capability_contract_negative() {
  negative_environment=$1
  negative_workspace_id=$2
  negative_sandbox_id=$3
  negative_sandbox_generation=$4
  negative_environment_profile_id=$5
  negative_provider=$6
  negative_prefix="compose-capability-$negative_environment-$negative_provider"
  for capability_mismatch_kind in mcp skill; do
    mismatch_session_id="session-$negative_prefix-mismatch-$capability_mismatch_kind"
    mismatch_output_file="$smoke_directory/$mismatch_session_id.json"
    mismatch_error_file="$smoke_directory/$mismatch_session_id.err"
    case "$capability_mismatch_kind" in
      mcp)
        mismatch_refs="--mcp-server-refs-json [{\"serverId\":\"$capability_mcp_id\",\"version\":\"9.9.9\",\"digest\":\"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\"}]"
        ;;
      skill)
        mismatch_refs="--skill-bundle-refs-json [{\"bundleId\":\"$capability_skill_id\",\"version\":\"9.9.9\",\"digest\":\"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\"}]"
        ;;
    esac
    if ! cloud_agentsctl_user --project "$project_id" --session "$mismatch_session_id" \
      --request-id "$negative_prefix-mismatch-$capability_mismatch_kind" \
      --idempotency-key "$negative_prefix-mismatch-$capability_mismatch_kind" \
      session create --provider "$negative_provider" --workspace "$negative_workspace_id" \
      --sandbox "$negative_sandbox_id" --sandbox-generation "$negative_sandbox_generation" \
      --environment-profile "$negative_environment_profile_id" --environment-profile-version 1 \
      >"$mismatch_output_file" 2>"$mismatch_error_file"; then
      cat "$mismatch_output_file" "$mismatch_error_file" >&2
      echo "Compose did not persist incompatible $capability_mismatch_kind reference for execution-time authorization" >&2
      exit 1
    fi
    mismatch_turn_id="turn-$negative_prefix-mismatch-$capability_mismatch_kind"
    mismatch_execution_id="execution-$negative_prefix-mismatch-$capability_mismatch_kind"
    cloud_agentsctl_user --project "$project_id" --session "$mismatch_session_id" --turn "$mismatch_turn_id" \
      --request-id "$negative_prefix-mismatch-$capability_mismatch_kind-turn" \
      --idempotency-key "$negative_prefix-mismatch-$capability_mismatch_kind-turn" \
      turn create --input "This execution must fail before Runtime opens." >/dev/null
    if cloud_agentsctl_user --project "$project_id" --session "$mismatch_session_id" --turn "$mismatch_turn_id" \
      --execution "$mismatch_execution_id" \
      --request-id "$negative_prefix-mismatch-$capability_mismatch_kind-execution" \
      --idempotency-key "$negative_prefix-mismatch-$capability_mismatch_kind-execution" \
      execution execute --runtime-mode full-access --interaction-mode default \
      $mismatch_refs --input "This execution must fail before Runtime opens." >"$mismatch_output_file" 2>"$mismatch_error_file"; then
      echo "Compose executed incompatible $capability_mismatch_kind capability" >&2
      exit 1
    fi
    if ! grep -Eiq 'CAPABILITY_UNAVAILABLE|capability_unavailable' "$mismatch_output_file" "$mismatch_error_file"; then
      cat "$mismatch_output_file" "$mismatch_error_file" >&2
      echo "Compose incompatible $capability_mismatch_kind did not fail closed" >&2
      exit 1
    fi
    mismatch_events_file="$smoke_directory/$mismatch_execution_id-events.json"
    cloud_agentsctl_user --project "$project_id" --session "$mismatch_session_id" \
      --request-id "$negative_prefix-mismatch-$capability_mismatch_kind-events" \
      events list --limit 64 >"$mismatch_events_file"
    CLOUD_AGENTS_COMPOSE_EVENTS_FILE="$mismatch_events_file" \
    CLOUD_AGENTS_COMPOSE_OPERATION="$capability_mismatch_kind.fail" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_FILE, "utf8"));
const event = value.events?.find((item) => item.spec?.operation === process.env.CLOUD_AGENTS_COMPOSE_OPERATION);
if (!event || event.spec?.result !== "failed" || event.spec?.errorCode !== "capability_version_mismatch") {
  console.error(JSON.stringify((value.events ?? []).map((item) => ({
    operation: item.spec?.operation,
    resource: item.spec?.resource,
    result: item.spec?.result,
    errorCode: item.spec?.errorCode,
    serverId: item.spec?.serverId,
    bundleId: item.spec?.bundleId,
  }))));
  throw new Error("incompatible capability did not produce the expected opaque failure event");
}
NODE
  done
  cross_tenant_capability_file="$smoke_directory/$negative_prefix-cross-tenant.json"
  cross_tenant_capability_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
    --config "$smoke_directory/admin-curl.conf" --request GET \
    --header "X-Request-ID: $negative_prefix-cross-tenant" \
    --output "$cross_tenant_capability_file" --write-out '%{http_code}' \
    "https://$endpoint/v1/admin/tenants/tenant-other/projects/$project_id/mcp-servers/$capability_mcp_id")
  case "$cross_tenant_capability_status" in
    401 | 403) ;;
    *)
      cat "$cross_tenant_capability_file" >&2
      echo "Compose cross-tenant capability read was accepted: status=$cross_tenant_capability_status" >&2
      exit 1
      ;;
  esac
  cross_tenant_skill_file="$smoke_directory/$negative_prefix-cross-tenant-skill.json"
  cross_tenant_skill_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
    --config "$smoke_directory/admin-curl.conf" --request GET \
    --header "X-Request-ID: $negative_prefix-cross-tenant-skill" \
    --output "$cross_tenant_skill_file" --write-out '%{http_code}' \
    "https://$endpoint/v1/admin/tenants/tenant-other/projects/$project_id/skill-bundles/$capability_skill_id")
  case "$cross_tenant_skill_status" in
    401 | 403) ;;
    *)
      cat "$cross_tenant_skill_file" >&2
      echo "Compose cross-tenant Skill Bundle read was accepted: status=$cross_tenant_skill_status" >&2
      exit 1
      ;;
  esac
  echo "capability_contract_negative=passed provider=$negative_provider environment=$negative_environment incompatible=mcp,skill cross_tenant_mcp_status=$cross_tenant_capability_status cross_tenant_skill_status=$cross_tenant_skill_status" >&2
}
assert_stale_capability_session() {
  stale_environment=$1
  stale_workspace_id=$2
  stale_sandbox_id=$3
  stale_generation=$4
  current_generation=$5
  stale_environment_profile_id=$6
  stale_provider=$7
  stale_capability_session_file="$smoke_directory/capability-$stale_environment-$stale_provider-stale-generation.json"
  stale_capability_session_body=$(printf '{"sessionId":"session-compose-capability-%s-%s-stale-generation","providerKind":"%s","workspaceId":"%s","sandboxId":"%s","sandboxGeneration":%s,"environmentProfileId":"%s","environmentProfileVersion":1,"mcpServerRefs":%s,"skillBundleRefs":%s}' \
    "$stale_environment" "$stale_provider" "$stale_provider" "$stale_workspace_id" "$stale_sandbox_id" "$stale_generation" \
    "$stale_environment_profile_id" "$capability_mcp_refs_json" "$capability_skill_refs_json")
  stale_capability_session_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
    --config "$smoke_directory/user-curl.conf" --request POST \
    --header 'Content-Type: application/json' \
    --header "X-Request-ID: compose-capability-$stale_environment-$stale_provider-stale-generation" \
    --header "Idempotency-Key: compose-capability-$stale_environment-$stale_provider-stale-generation" \
    --data "$stale_capability_session_body" --output "$stale_capability_session_file" --write-out '%{http_code}' \
    "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/sessions")
  if [ "$stale_capability_session_status" != 409 ] || ! CLOUD_AGENTS_COMPOSE_RESPONSE_FILE="$stale_capability_session_file" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RESPONSE_FILE,"utf8"));process.exit(value.error?.code==="SESSION_CONFLICT"?0:1)'; then
    cat "$stale_capability_session_file" >&2
    echo "Compose accepted a capability Session bound to stale $stale_environment Sandbox generation $stale_generation" >&2
    exit 1
  fi
  echo "capability_stale_generation=passed provider=$stale_provider environment=$stale_environment old_generation=$stale_generation current_generation=$current_generation status=$stale_capability_session_status" >&2
}
if [ "$capability_negative_test" -eq 1 ]; then
  for negative_provider in $real_provider_kinds; do
    run_capability_contract_negative docker "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" \
      "$foundation_agent_generation" "$foundation_agent_environment_profile_id" "$negative_provider"
  done
fi
remote_agent_workspace_id=
remote_agent_sandbox_id=
remote_agent_runtime_id=
remote_agent_generation=
remote_agent_resource_version=
remote_agent_environment_profile_id=
remote_agent_target=
if [ "$remote_runtime" -eq 1 ]; then
  remote_agent_workspace_id=compose-remote-agent-workspace
  remote_agent_sandbox_id=compose-remote-agent-sandbox
  remote_agent_runtime_profile_id=compose-remote-agent-profile
  remote_agent_environment_profile_id=compose-remote-agent-environment-profile
  remote_agent_target=$remote_target_id
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
    remote_agent_state=${remote_agent_values%%|*}
    remote_agent_rest=${remote_agent_values#*|}
    remote_agent_generation=${remote_agent_rest%%|*}
    remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_resource_version=${remote_agent_rest%%|*}
    remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_runtime_id=${remote_agent_rest%%|*}
    remember_opensandbox_runtime_id "$remote_agent_runtime_id"
    remote_agent_error=${remote_agent_rest#*|}
    if [ "$remote_agent_state" = running ]; then
      [ -n "$remote_agent_runtime_id" ] || { echo "Compose outbound RemoteWorker Agent Sandbox is running without a Runtime id" >&2; exit 1; }
      break
    fi
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
  if [ "$capability_negative_test" -eq 1 ]; then
    for negative_provider in $real_provider_kinds; do
      run_capability_contract_negative remote-worker "$remote_agent_workspace_id" "$remote_agent_sandbox_id" \
        "$remote_agent_generation" "$remote_agent_environment_profile_id" "$negative_provider"
    done
  fi
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
      command: ["/bin/sh", "-c", "sleep 3600"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  volumes:
    - name: workspace
      persistentVolumeClaim:
        claimName: $bind_volume_name
EOF
    kubernetes_ctl -n "$bind_namespace" wait pod/ca-workspace-binder \
      --for=jsonpath='{.status.phase}'=Running --timeout=120s >/dev/null
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
    kubernetes_agent_state=${kubernetes_agent_values%%|*}
    kubernetes_agent_rest=${kubernetes_agent_values#*|}
    kubernetes_agent_generation=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_resource_version=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_runtime_id=${kubernetes_agent_rest%%|*}
    remember_opensandbox_runtime_id "$kubernetes_agent_runtime_id"
    kubernetes_agent_error=${kubernetes_agent_rest#*|}
    if [ "$cross_node_recovery" -eq 1 ] && [ "$cross_node_environment" = kubernetes ] && [ "$kubernetes_source_volume_bound" -eq 0 ]; then
      kubernetes_agent_volume=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-agent-sandbox.json" node -e \
        'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(value.spec?.physicalVolumeId??"")')
      if [ -n "$kubernetes_agent_volume" ]; then
        kubernetes_bind_workspace_volume "$kubernetes_runtime_namespace" "$kubernetes_agent_volume" "$kubernetes_source_node"
        kubernetes_source_volume_bound=1
      fi
    fi
    if [ "$kubernetes_agent_state" = running ]; then
      [ -n "$kubernetes_agent_runtime_id" ] || { echo "Compose Kubernetes Agent Sandbox is running without a Runtime id" >&2; exit 1; }
      break
    fi
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
  if [ "$capability_negative_test" -eq 1 ]; then
    for negative_provider in $real_provider_kinds; do
      run_capability_contract_negative kubernetes "$kubernetes_agent_workspace_id" "$kubernetes_agent_sandbox_id" \
        "$kubernetes_agent_generation" "$kubernetes_agent_environment_profile_id" "$negative_provider"
    done
  fi
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

execute_real_provider_with_mcp_approvals() {
  approval_session_id=$1
  approval_turn_id=$2
  approval_execution_id=$3
  approval_request_prefix=$4
  approval_prompt=$5
  approval_execution_file=$6
  approval_expected_command=${7-}
  approval_runtime_mode=${8:-approval-required}
  approval_execution_idempotency_key=${9:-$approval_request_prefix}
  approval_status_file="$approval_execution_file.status"
  approval_error_file="$approval_execution_file.stderr"
  approval_current_file="$approval_execution_file.current"
  (
    set +e
    cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$approval_session_id" --turn "$approval_turn_id" \
      --execution "$approval_execution_id" --request-id "$approval_request_prefix" \
      --idempotency-key "$approval_execution_idempotency_key" execution execute \
      --runtime-mode "$approval_runtime_mode" --interaction-mode default $capability_execution_flags --input "$approval_prompt" \
      >"$approval_execution_file" 2>"$approval_error_file"
    printf '%s\n' "$?" >"$approval_status_file"
  ) &
  approval_execute_pid=$!
  handled_approval_requests=
  attempt=0
  while [ ! -f "$approval_status_file" ]; do
    if [ -s "$approval_current_file" ]; then unlink "$approval_current_file" 2>/dev/null || true; fi
    if cloud_agentsctl_user --project "$project_id" --session "$approval_session_id" --turn "$approval_turn_id" \
      --execution "$approval_execution_id" --request-id "$approval_request_prefix-poll-$attempt" execution get \
      >"$approval_current_file" 2>/dev/null; then
      approval_request=$(CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$approval_current_file" \
        CLOUD_AGENTS_COMPOSE_HANDLED_APPROVALS="$handled_approval_requests" \
        CLOUD_AGENTS_COMPOSE_EXPECTED_COMMAND="$approval_expected_command" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
const handled = new Set((process.env.CLOUD_AGENTS_COMPOSE_HANDLED_APPROVALS ?? "").split(" ").filter(Boolean));
const expectedCommand = process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_COMMAND;
const normalizeShellCommand = (command) => typeof command === "string"
  ? command.replace(/^\/bin\/bash -l?c /, "/bin/bash -c ") : command;
for (const message of value.messages ?? []) {
  if (message.messageType !== "InteractionRequest" || message.payload?.interactionType !== "approval") continue;
  const requestId = message.payload?.requestId;
  const categories = message.payload?.sensitiveAction?.categories;
  const isMcpApproval = Array.isArray(categories) && categories.includes("external-mcp-action");
  const isSkillApproval = message.payload?.toolName === "Skill" && message.payload?.requestKind === "tool";
  const isArtifactWriteApproval = message.payload?.toolName === "Write";
  const isExpectedCodexCommand = expectedCommand && message.payload?.provider === "codex" &&
    message.payload?.requestKind === "command" &&
    normalizeShellCommand(message.payload?.command) === normalizeShellCommand(expectedCommand);
  const isCapabilityApproval = (isMcpApproval || isSkillApproval || isArtifactWriteApproval || isExpectedCodexCommand);
  const managedSkillQuote = String.fromCharCode(39);
  const managedSkillCommand = message.payload?.command ?? "";
  const isManagedSkillRead = message.payload?.provider === "codex" &&
    message.payload?.requestKind === "command" &&
    managedSkillCommand.startsWith("/bin/bash -lc ") &&
    managedSkillCommand.includes("/tmp/cloud-agents-skills/") &&
    managedSkillCommand.includes("/skills/managed-capability-acceptance/SKILL.md") &&
    managedSkillCommand.endsWith("SKILL.md" + managedSkillQuote);
  if (typeof requestId === "string" && !handled.has(requestId) &&
      (isCapabilityApproval || isManagedSkillRead) &&
      Number.isSafeInteger(value.spec?.generation)) {
    process.stdout.write(`${value.spec.generation}|${requestId}`);
    break;
  }
}
NODE
      ) || true
      if [ -n "$approval_request" ]; then
        approval_generation=${approval_request%%|*}
        approval_request_id=${approval_request#*|}
        cloud_agentsctl_user --project "$project_id" --session "$approval_session_id" --turn "$approval_turn_id" \
          --execution "$approval_execution_id" --request-id "$approval_request_prefix-approve-$attempt" \
          --idempotency-key "$approval_request_prefix-approve-$attempt" execution resolve-approval \
          --generation "$approval_generation" --interaction-request "$approval_request_id" --decision accept >/dev/null
        handled_approval_requests="$handled_approval_requests $approval_request_id"
      fi
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 1200 ]; then
      echo "Compose real Claude capability execution approval timed out" >&2
      kill "$approval_execute_pid" >/dev/null 2>&1 || true
      wait "$approval_execute_pid" 2>/dev/null || true
      return 1
    fi
    sleep 0.5
  done
  wait "$approval_execute_pid" 2>/dev/null || true
  approval_status=$(cat "$approval_status_file")
  approval_execute_pid=
  if [ "$approval_status" -ne 0 ]; then
    cloud_agentsctl_user --project "$project_id" --session "$approval_session_id" --turn "$approval_turn_id" \
      --execution "$approval_execution_id" --request-id "$approval_request_prefix-failure" execution get \
      >"$approval_current_file" 2>/dev/null || true
    failure_fixture_directory=
    if sync_capability_mcp_fixture; then failure_fixture_directory="$smoke_directory/mcp-side-effect"; fi
    CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$approval_current_file" \
      CLOUD_AGENTS_COMPOSE_FAILURE_FIXTURE_DIRECTORY="$failure_fixture_directory" node <<'NODE'
const { existsSync, readFileSync } = require("node:fs");
const { join } = require("node:path");
const { createHash } = require("node:crypto");
let value;
try { value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8")); }
catch { console.error("capability_execution_failure=unavailable"); process.exit(0); }
const code = (value) => typeof value === "string" && /^[a-z][a-z0-9_.-]{0,79}$/.test(value) ? value : undefined;
const errors = [value.spec?.errorMessage, ...(value.messages ?? []).flatMap((message) =>
  [message.error?.message, message.payload?.error?.message])].filter((text) => typeof text === "string");
const fixtureDirectory = process.env.CLOUD_AGENTS_COMPOSE_FAILURE_FIXTURE_DIRECTORY;
const count = (name) => !fixtureDirectory ? undefined : existsSync(join(fixtureDirectory, name))
  ? readFileSync(join(fixtureDirectory, name), "utf8").split("\n").filter(Boolean).length : 0;
console.error(JSON.stringify({ capabilityExecutionFailure: {
  state: code(value.spec?.state), errorCode: code(value.spec?.errorCode),
  fixtureRequests: count("requests.log"), fixtureSideEffects: count("result.txt"),
  errorDigests: errors.map((text) => createHash("sha256").update(text).digest("hex")),
  errorHints: {
    skillSandbox: errors.some((text) => /Managed Skill|Skill Bundle/.test(text)),
    permissionDenied: errors.some((text) => /EACCES|EPERM|permission denied/i.test(text)),
    skillSetMismatch: errors.some((text) => /unexpected managed Skill set/.test(text)),
    pluginSetMismatch: errors.some((text) => /unexpected managed Skill plugin/.test(text)),
  },
  messages: (value.messages ?? []).map((message) => ({
    type: code(message.messageType?.toLowerCase()), event: code(message.payload?.eventType),
    errorCode: code(message.error?.code), status: code(message.payload?.payload?.status),
  })),
} }));
NODE
    return 1
  fi
  [ "$approval_status" -eq 0 ]
}

execute_real_provider_with_safe_retry() {
  provider_kind=$1
  retry_session_id=$2
  retry_turn_id=$3
  base_execution_id=$4
  request_prefix=$5
  retry_prompt=$6
  execution_file=$7
  approval_expected_command=${8-}
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
    if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
      if execute_real_provider_with_mcp_approvals "$retry_session_id" "$attempt_turn_id" "$attempt_execution_id" \
        "$request_prefix$attempt_request_suffix" "$retry_prompt" "$execution_file" "$approval_expected_command"; then
        completed_real_provider_turn_id=$attempt_turn_id
        completed_real_provider_execution_id=$attempt_execution_id
        return 0
      fi
    elif cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$retry_session_id" --turn "$attempt_turn_id" \
        --execution "$attempt_execution_id" --request-id "$request_prefix$attempt_request_suffix" \
        --idempotency-key "$request_prefix$attempt_request_suffix" execution execute \
        --runtime-mode full-access --interaction-mode default $capability_execution_flags --input "$retry_prompt" >"$execution_file"; then
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

run_capability_process_recovery() {
  recovery_provider=$1
  [ "$capability_process_recovery" -eq 1 ] || return 0
  if [ -n "$capability_process_recovery_environment" ] &&
    [ "$capability_process_recovery_environment" != "$real_provider_environment_slug" ]; then
    return 0
  fi
  capability_process_recovery_ran=1
  run_capability_process_recovery_fault() {
    recovery_fault=$1
    interaction_output_directory="$smoke_directory/agent-interactions-$real_provider_environment_slug-capability-recovery-$recovery_fault"
    mkdir -m 0700 "$interaction_output_directory"
    recovery_checkpoint_mode=interaction
    recovery_artifact_path=
    recovery_expected_content=
    recovery_bash_command=
    recovery_mcp_tool_name=
    recovery_skill_name=managed-capability-acceptance
    case "$recovery_provider" in
      pi | deepseek-harness)
        recovery_checkpoint_mode=side-effect
        recovery_artifact_path=".cloud-agents-stage3-acceptance/$real_provider_environment_slug-process-$recovery_provider-$recovery_fault.txt"
        recovery_expected_content="cloud-agents $real_provider_environment_label $recovery_provider $recovery_fault process recovery"
        recovery_bash_command="mkdir -p '${recovery_artifact_path%/*}' && printf '%s\\n' '$recovery_expected_content' > '$recovery_artifact_path' && sleep 60"
        if [ "$recovery_provider" = pi ]; then
          recovery_mcp_tool_name=$(CAPABILITY_MCP_RESOURCE_ID="$capability_mcp_id" node <<'NODE'
const { createHash } = require("node:crypto");
const resourceId = process.env.CAPABILITY_MCP_RESOURCE_ID;
const toolName = "acceptance_marker";
const safe = (value) => value.replaceAll(/[^A-Za-z0-9_-]/gu, "_");
const suffix = createHash("sha256").update(`${resourceId}\u0000${toolName}`).digest("hex").slice(0, 8);
process.stdout.write(`mcp__${safe(resourceId).slice(0, 16)}__${safe(toolName).slice(0, 24)}__${suffix}`);
NODE
)
        else
          recovery_bash_command="sleep 60 && mkdir -p '${recovery_artifact_path%/*}' && printf '%s\\n' '$recovery_expected_content' > '$recovery_artifact_path'"
          recovery_mcp_tool_name=$(CAPABILITY_MCP_RESOURCE_ID="$capability_mcp_id" node <<'NODE'
const { createHash } = require("node:crypto");
const resourceId = process.env.CAPABILITY_MCP_RESOURCE_ID;
const normalized = resourceId.replace(/[^A-Za-z0-9_-]/gu, "_").slice(0, 19) || "server";
const suffix = createHash("sha256").update(resourceId).digest("hex").slice(0, 8);
process.stdout.write(`mcp__ca_${normalized}_${suffix}__acceptance_marker`);
NODE
)
        fi
        ;;
    esac
    if [ "$recovery_fault" = worker ]; then
      recovery_lease_id=
      recovery_workspace_id=
      recovery_sandbox_id=
      recovery_generation=
      recovery_environment_profile_id=
      recovery_target_id=
      case "$real_provider_environment_slug" in
        docker)
          recovery_lease_id=$profile_environment_id
          recovery_worker_container=$(docker ps -q \
            --filter label=cloud-agents.dev/managed=true \
            --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
            --filter label=cloud-agents.dev/project="$project_id" \
            --filter label=cloud-agents.dev/target=docker-compose-target \
            --filter label=cloud-agents.dev/lease="$profile_environment_id")
          ;;
        remote-worker)
          recovery_workspace_id=$real_provider_workspace_id
          recovery_sandbox_id=$real_provider_sandbox_id
          recovery_generation=$real_provider_sandbox_generation
          recovery_environment_profile_id=$real_provider_environment_profile_id
          recovery_target_id=$remote_target_id
          recovery_worker_container=$remote_worker_container
          ;;
        *) echo "Kubernetes direct Sandbox has no bound Worker fault target" >&2; return 1 ;;
      esac
      if printf '%s\n' "$recovery_worker_container" | grep -Eq '^[0-9a-f]{12}$'; then
        recovery_worker_container=$(docker inspect --format '{{.Id}}' "$recovery_worker_container")
      elif [ "$real_provider_environment_slug" = remote-worker ]; then
        recovery_worker_container=$(docker inspect --format '{{.Id}}' "$recovery_worker_container")
      fi
      if ! printf '%s\n' "$recovery_worker_container" | grep -Eq '^[0-9a-f]{64}$'; then
        echo "capability recovery could not resolve the lease Worker" >&2
        return 1
      fi
      if [ "$real_provider_environment_slug" = docker ]; then
        start_capability_mcp_fixture "$recovery_worker_container"
      else
        start_capability_mcp_fixture "$remote_agent_runtime_id"
      fi
      CLOUD_AGENTS_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_PROJECT="$project_id" \
      CLOUD_AGENTS_E2E_LEASE_ID="$recovery_lease_id" \
      CLOUD_AGENTS_E2E_WORKSPACE_ID="$recovery_workspace_id" \
      CLOUD_AGENTS_E2E_SANDBOX_ID="$recovery_sandbox_id" \
      CLOUD_AGENTS_E2E_SANDBOX_GENERATION="$recovery_generation" \
      CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_ID="$recovery_environment_profile_id" \
      CLOUD_AGENTS_E2E_ENVIRONMENT="$real_provider_environment_slug" \
      CLOUD_AGENTS_E2E_RUN_ID="compose-capability-process-recovery-$real_provider_environment_slug-worker-$project" \
      CLOUD_AGENTS_E2E_OUTPUT_DIR="$interaction_output_directory" \
      CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER="$(compose ps -q control-plane)" \
      CLOUD_AGENTS_E2E_WORKER_CONTAINER="$recovery_worker_container" \
      CLOUD_AGENTS_E2E_AGENT_TARGET_ID="$recovery_target_id" \
      CLOUD_AGENTS_E2E_MCP_FIXTURE_CONTAINER="$mcp_fixture_container" \
      CLOUD_AGENTS_E2E_POSTGRES_CONTAINER="$(compose ps -q postgres)" \
      CLOUD_AGENTS_E2E_AUTH_CONFIG="$smoke_directory/auth.json" \
      CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY="$smoke_directory/auth-test-private-key.pem" \
      CLOUD_AGENTS_E2E_ADMIN_TOKEN_FILE="$smoke_directory/admin-token" \
      CLOUD_AGENTS_E2E_ADMIN_CURL_CONFIG="$smoke_directory/admin-curl.conf" \
      CLOUD_AGENTS_E2E_RECOVERY_ONLY=1 \
      CLOUD_AGENTS_E2E_RECOVERY_FAULT=worker \
      CLOUD_AGENTS_E2E_RECOVERY_PROVIDER="$recovery_provider" \
      CLOUD_AGENTS_E2E_RECOVERY_CHECKPOINT_MODE="$recovery_checkpoint_mode" \
      CLOUD_AGENTS_E2E_RECOVERY_ARTIFACT_PATH="$recovery_artifact_path" \
      CLOUD_AGENTS_E2E_RECOVERY_EXPECTED_CONTENT="$recovery_expected_content" \
      CLOUD_AGENTS_E2E_RECOVERY_BASH_COMMAND="$recovery_bash_command" \
      CLOUD_AGENTS_E2E_RECOVERY_MCP_TOOL_NAME="$recovery_mcp_tool_name" \
      CLOUD_AGENTS_E2E_RECOVERY_SKILL_NAME="$recovery_skill_name" \
      CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON="$capability_mcp_refs_json" \
      CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON="$capability_skill_refs_json" \
      CLOUD_AGENTSCTL="$cli" \
        sh "$script_directory/test-platform-agent-interactions.sh" || return 1
    else
      recovery_worker_container=
      case "$real_provider_environment_slug" in
        docker) recovery_runtime_id=$foundation_agent_runtime_id; recovery_target_id=docker-compose-target ;;
        remote-worker)
          recovery_runtime_id=$remote_agent_runtime_id
          recovery_target_id=$remote_target_id
          recovery_worker_container=$(docker inspect --format '{{.Id}}' "$remote_worker_container")
          ;;
        kubernetes) recovery_runtime_id=$kubernetes_agent_runtime_id; recovery_target_id=$kubernetes_runtime_target_id ;;
      esac
      start_capability_mcp_fixture "$recovery_runtime_id"
      CLOUD_AGENTS_ENDPOINT="https://$endpoint" \
      CLOUD_AGENTS_CA_FILE="$smoke_directory/ca.crt" \
      CLOUD_AGENTS_TOKEN_FILE="$smoke_directory/user-token" \
      CLOUD_AGENTS_TENANT=tenant-compose-smoke \
      CLOUD_AGENTS_PROJECT="$project_id" \
      CLOUD_AGENTS_E2E_WORKSPACE_ID="$real_provider_workspace_id" \
      CLOUD_AGENTS_E2E_SANDBOX_ID="$real_provider_sandbox_id" \
      CLOUD_AGENTS_E2E_SANDBOX_GENERATION="$real_provider_sandbox_generation" \
      CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_ID="$real_provider_environment_profile_id" \
      CLOUD_AGENTS_E2E_ENVIRONMENT="$real_provider_environment_slug" \
      CLOUD_AGENTS_E2E_RUN_ID="compose-capability-process-recovery-$real_provider_environment_slug-agent-$project" \
      CLOUD_AGENTS_E2E_OUTPUT_DIR="$interaction_output_directory" \
      CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER="$(compose ps -q control-plane)" \
      CLOUD_AGENTS_E2E_WORKER_CONTAINER="$recovery_worker_container" \
      CLOUD_AGENTS_E2E_POSTGRES_CONTAINER="$(compose ps -q postgres)" \
      CLOUD_AGENTS_E2E_AUTH_CONFIG="$smoke_directory/auth.json" \
      CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY="$smoke_directory/auth-test-private-key.pem" \
      CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID="$recovery_runtime_id" \
      CLOUD_AGENTS_E2E_AGENT_TARGET_ID="$recovery_target_id" \
      CLOUD_AGENTS_E2E_RECOVERY_ONLY=1 \
      CLOUD_AGENTS_E2E_RECOVERY_FAULT=agent \
      CLOUD_AGENTS_E2E_RECOVERY_PROVIDER="$recovery_provider" \
      CLOUD_AGENTS_E2E_RECOVERY_CHECKPOINT_MODE="$recovery_checkpoint_mode" \
      CLOUD_AGENTS_E2E_RECOVERY_ARTIFACT_PATH="$recovery_artifact_path" \
      CLOUD_AGENTS_E2E_RECOVERY_EXPECTED_CONTENT="$recovery_expected_content" \
      CLOUD_AGENTS_E2E_RECOVERY_BASH_COMMAND="$recovery_bash_command" \
      CLOUD_AGENTS_E2E_RECOVERY_MCP_TOOL_NAME="$recovery_mcp_tool_name" \
      CLOUD_AGENTS_E2E_RECOVERY_SKILL_NAME="$recovery_skill_name" \
      CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON="$capability_mcp_refs_json" \
      CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON="$capability_skill_refs_json" \
      CLOUD_AGENTSCTL="$cli" \
        sh "$script_directory/test-platform-agent-interactions.sh" || return 1
    fi
  }
  case "$capability_process_recovery_faults" in
    worker) run_capability_process_recovery_fault worker || return 1 ;;
    agent) run_capability_process_recovery_fault agent || return 1 ;;
    both)
      run_capability_process_recovery_fault worker || return 1
      run_capability_process_recovery_fault agent || return 1
      ;;
  esac
  echo "capability_process_recovery=passed provider=$recovery_provider environment=$real_provider_environment_slug faults=$capability_process_recovery_faults" >&2
}

run_capability_transport_recovery() {
  transport_provider_kind=$1
  transport_provider_slug=$2
  [ "$capability_transport_recovery" -eq 1 ] || return 0
  [ "$capability_transport_recovery_ran" -eq 0 ] || return 0
  if [ -n "$capability_transport_recovery_environment" ] &&
    [ "$capability_transport_recovery_environment" != "$real_provider_environment_slug" ]; then
    return 0
  fi
  capability_transport_recovery_ran=1
  transport_prefix="compose-capability-transport-$real_provider_environment_slug-$transport_provider_slug"
  transport_session_id="session-$transport_prefix"
  transport_fault_turn_id="turn-$transport_prefix-disconnect"
  transport_fault_execution_id="execution-$transport_prefix-disconnect"
  transport_reconnect_turn_id="turn-$transport_prefix-reconnect"
  transport_reconnect_execution_id="execution-$transport_prefix-reconnect"
  transport_side_effect_tool=acceptance_side_effect
  transport_marker_tool=acceptance_marker
  if [ "$transport_provider_kind" = pi ]; then
    transport_side_effect_tool=$(CLOUD_AGENTS_COMPOSE_RESOURCE_ID="$capability_mcp_id" \
      CLOUD_AGENTS_COMPOSE_TOOL_NAME=acceptance_side_effect node -e \
      'const {createHash}=require("node:crypto");const resource=process.env.CLOUD_AGENTS_COMPOSE_RESOURCE_ID;const tool=process.env.CLOUD_AGENTS_COMPOSE_TOOL_NAME;const safe=(value)=>value.toLowerCase().replace(/[^a-z0-9_-]+/g,"-").replace(/^-+|-+$/g,"");const suffix=createHash("sha256").update(`${resource}\u0000${tool}`).digest("hex").slice(0,8);process.stdout.write(`mcp__${safe(resource).slice(0,16)||"server"}__${safe(tool).slice(0,24)||"tool"}__${suffix}`)')
    transport_marker_tool=$(CLOUD_AGENTS_COMPOSE_RESOURCE_ID="$capability_mcp_id" \
      CLOUD_AGENTS_COMPOSE_TOOL_NAME=acceptance_marker node -e \
      'const {createHash}=require("node:crypto");const resource=process.env.CLOUD_AGENTS_COMPOSE_RESOURCE_ID;const tool=process.env.CLOUD_AGENTS_COMPOSE_TOOL_NAME;const safe=(value)=>value.toLowerCase().replace(/[^a-z0-9_-]+/g,"-").replace(/^-+|-+$/g,"");const suffix=createHash("sha256").update(`${resource}\u0000${tool}`).digest("hex").slice(0,8);process.stdout.write(`mcp__${safe(resource).slice(0,16)||"server"}__${safe(tool).slice(0,24)||"tool"}__${suffix}`)')
  fi
  transport_fault_prompt="Call the managed MCP tool named $transport_side_effect_tool exactly once. Do not call any other tool and do not retry it. Wait for its result; if the transport disconnects, fail closed without claiming success."
  transport_reconnect_prompt="Call the managed MCP tool named $transport_marker_tool exactly once. Do not call any other tool. Reply done only after it succeeds."

  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" \
    --request-id "$transport_prefix-session" --idempotency-key "$transport_prefix-session" \
    session create --provider "$transport_provider_kind" $capability_session_flags --workspace "$real_provider_workspace_id" \
    --sandbox "$real_provider_sandbox_id" --sandbox-generation "$real_provider_sandbox_generation" \
    --environment-profile "$real_provider_environment_profile_id" --environment-profile-version 1 >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --request-id "$transport_prefix-disconnect-turn" --idempotency-key "$transport_prefix-disconnect-turn" \
    turn create --input "$transport_fault_prompt" >/dev/null

  sync_capability_mcp_fixture
  transport_side_effects_before=$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')
  transport_requests_before=$(capability_mcp_tool_call_count)
  arm_capability_mcp_disconnect
  transport_fault_file="$smoke_directory/$transport_fault_execution_id.json"
  if execute_real_provider_with_mcp_approvals "$transport_session_id" "$transport_fault_turn_id" \
    "$transport_fault_execution_id" "$transport_prefix-disconnect-execution" "$transport_fault_prompt" "$transport_fault_file"; then
    echo "capability transport fault returned success instead of failing closed" >&2
    return 1
  fi
  wait_capability_mcp_disconnect_commit
  transport_fault_terminal="$smoke_directory/$transport_fault_execution_id-terminal.json"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-terminal" \
    execution get >"$transport_fault_terminal"
  transport_fault_events="$smoke_directory/$transport_fault_execution_id-events.json"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" \
    --request-id "$transport_prefix-disconnect-events" events list --limit 64 >"$transport_fault_events"
  transport_side_effects_after_fault=$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')
  transport_requests_after_fault=$(capability_mcp_tool_call_count)
  test "$transport_side_effects_after_fault" -eq $((transport_side_effects_before + 1))
  test "$transport_requests_after_fault" -eq $((transport_requests_before + 1))
  sleep 2
  test "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')" -eq "$transport_side_effects_after_fault"
  test "$(capability_mcp_tool_call_count)" -eq "$transport_requests_after_fault"

  start_capability_mcp_fixture "$mcp_fixture_runtime_id" "$mcp_fixture_docker_host" 1
  assert_capability_mcp_listener_from_runtime_netns
  attempt=0
  while :; do
    cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
      --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-expiry" \
      execution get >"$transport_fault_terminal"
    if CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$transport_fault_terminal" node -e \
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE,"utf8"));process.exit(Date.parse(value.spec?.claimExpiresAt??"")+1000<Date.now()?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "capability transport recovery claim did not expire" >&2
      return 1
    fi
    sleep 1
  done
  transport_blocked_file="$smoke_directory/$transport_fault_execution_id-blocked.log"
  set +e
  cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-execution" \
    --idempotency-key "$transport_prefix-disconnect-execution" execution execute \
    --runtime-mode approval-required --interaction-mode default $capability_execution_flags --input "$transport_fault_prompt" \
    >"$transport_blocked_file" 2>&1
  transport_blocked_status=$?
  set -e
  if [ "$transport_blocked_status" -eq 0 ]; then
    echo "capability transport recovery replay bypassed side-effect reconciliation" >&2
    return 1
  fi
  if ! grep -Fq 'RECOVERY_REQUIRES_RECONCILIATION' "$transport_blocked_file"; then
    echo "capability transport recovery replay returned the wrong stable error code" >&2
    return 1
  fi
  transport_fault_reconcile="$smoke_directory/$transport_fault_execution_id-awaiting-reconciliation.json"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-reconcile-state" \
    execution get >"$transport_fault_reconcile"
  transport_reconcile_values=$(CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$transport_fault_reconcile" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE,"utf8"));const checkpoint=value.spec?.checkpoint;if(value.spec?.state!=="running"||value.spec?.recoveryState!=="awaiting_reconciliation"||value.spec?.recoveryReason!=="side_effect_outcome_unknown"||checkpoint?.pendingSideEffect!==true||!checkpoint?.digest)process.exit(1);process.stdout.write(`${value.spec.generation}|${checkpoint.digest}`)')
  transport_reconcile_generation=${transport_reconcile_values%%|*}
  transport_reconcile_checkpoint_digest=${transport_reconcile_values#*|}
  sync_capability_mcp_fixture
  test "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')" -eq "$transport_side_effects_after_fault"
  test "$(capability_mcp_tool_call_count)" -eq "$transport_requests_after_fault"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-reconcile" \
    --idempotency-key "$transport_prefix-disconnect-reconcile" execution reconcile \
    --generation "$transport_reconcile_generation" --checkpoint-digest "$transport_reconcile_checkpoint_digest" \
    --outcome confirmed >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-cancel" \
    --idempotency-key "$transport_prefix-disconnect-cancel" execution cancel \
    --generation "$transport_reconcile_generation" >/dev/null
  transport_fault_cancelled="$smoke_directory/$transport_fault_execution_id-cancelled.json"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_fault_turn_id" \
    --execution "$transport_fault_execution_id" --request-id "$transport_prefix-disconnect-cancelled" \
    execution get >"$transport_fault_cancelled"

  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" --turn "$transport_reconnect_turn_id" \
    --request-id "$transport_prefix-reconnect-turn" --idempotency-key "$transport_prefix-reconnect-turn" \
    turn create --input "$transport_reconnect_prompt" >/dev/null
  transport_reconnect_file="$smoke_directory/$transport_reconnect_execution_id.json"
  if ! execute_real_provider_with_mcp_approvals "$transport_session_id" "$transport_reconnect_turn_id" \
    "$transport_reconnect_execution_id" "$transport_prefix-reconnect-execution" "$transport_reconnect_prompt" "$transport_reconnect_file"; then
    return 1
  fi
  sync_capability_mcp_fixture
  test "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')" -eq "$transport_side_effects_after_fault"
  test "$(capability_mcp_tool_call_count)" -eq $((transport_requests_after_fault + 1))
  transport_events="$smoke_directory/$transport_reconnect_execution_id-events.json"
  cloud_agentsctl_user --project "$project_id" --session "$transport_session_id" \
    --request-id "$transport_prefix-reconnect-events" events list --limit 64 >"$transport_events"

  CLOUD_AGENTS_COMPOSE_FAULT_EXECUTION_FILE="$transport_fault_reconcile" \
  CLOUD_AGENTS_COMPOSE_CANCELLED_EXECUTION_FILE="$transport_fault_cancelled" \
  CLOUD_AGENTS_COMPOSE_RECONNECT_EXECUTION_FILE="$transport_reconnect_file" \
  CLOUD_AGENTS_COMPOSE_FAULT_EVENTS_FILE="$transport_fault_events" \
  CLOUD_AGENTS_COMPOSE_EVENTS_FILE="$transport_events" \
  CLOUD_AGENTS_COMPOSE_CAPABILITY_DESCRIPTOR="$capability_descriptor" \
  CLOUD_AGENTS_COMPOSE_MCP_ID="$capability_mcp_id" \
  CLOUD_AGENTS_COMPOSE_MCP_VERSION="$capability_mcp_version" \
  CLOUD_AGENTS_COMPOSE_MCP_DIGEST="$capability_mcp_digest" \
  CLOUD_AGENTS_COMPOSE_SKILL_ID="$capability_skill_id" \
  CLOUD_AGENTS_COMPOSE_SKILL_VERSION="$capability_skill_version" \
  CLOUD_AGENTS_COMPOSE_SKILL_DIGEST="$capability_skill_digest" \
  CLOUD_AGENTS_COMPOSE_MARKER="$mcp_fixture_marker" \
  CLOUD_AGENTS_COMPOSE_FAULT_PROMPT="$transport_fault_prompt" \
  CLOUD_AGENTS_COMPOSE_RECONNECT_PROMPT="$transport_reconnect_prompt" node <<'NODE'
const { readFileSync } = require("node:fs");
const fault = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_FAULT_EXECUTION_FILE, "utf8"));
const cancelled = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_CANCELLED_EXECUTION_FILE, "utf8"));
const reconnect = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RECONNECT_EXECUTION_FILE, "utf8"));
const faultPage = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_FAULT_EVENTS_FILE, "utf8"));
const page = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_FILE, "utf8"));
const descriptor = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_DESCRIPTOR, "utf8"));
const mcpId = process.env.CLOUD_AGENTS_COMPOSE_MCP_ID;
const skillId = process.env.CLOUD_AGENTS_COMPOSE_SKILL_ID;
const refsMatch = (value) => value.spec?.mcpServerRefs?.length === 1 &&
  value.spec.mcpServerRefs[0]?.serverId === mcpId &&
  value.spec.mcpServerRefs[0]?.version === process.env.CLOUD_AGENTS_COMPOSE_MCP_VERSION &&
  value.spec.mcpServerRefs[0]?.digest === process.env.CLOUD_AGENTS_COMPOSE_MCP_DIGEST &&
  value.spec?.skillBundleRefs?.length === 1 && value.spec.skillBundleRefs[0]?.bundleId === skillId &&
  value.spec.skillBundleRefs[0]?.version === process.env.CLOUD_AGENTS_COMPOSE_SKILL_VERSION &&
  value.spec.skillBundleRefs[0]?.digest === process.env.CLOUD_AGENTS_COMPOSE_SKILL_DIGEST;
if (fault.spec?.state !== "running" || fault.spec?.recoveryState !== "awaiting_reconciliation" ||
    fault.spec?.recoveryReason !== "side_effect_outcome_unknown" ||
    fault.spec?.checkpoint?.pendingSideEffect !== true || !refsMatch(fault)) {
  throw new Error("transport-disconnected capability execution did not fail closed with the original refs");
}
if (cancelled.spec?.state !== "cancelled" || cancelled.spec?.recoveryState !== "none" ||
    cancelled.spec?.recoveryReason !== undefined ||
    cancelled.spec?.checkpoint?.pendingSideEffect !== false || !refsMatch(cancelled)) {
  throw new Error("reconciled capability execution did not reach a cleared cancelled terminal state");
}
const completedMcp = (reconnect.messages ?? []).some((message) =>
  message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
  message.payload?.payload?.itemType === "mcp_tool_call" &&
  message.payload?.payload?.status === "completed" &&
  message.payload?.payload?.data?.capabilityResourceId === mcpId);
if (reconnect.spec?.state !== "succeeded" || !refsMatch(reconnect) || !completedMcp) {
  throw new Error("reconnected capability execution did not succeed with the original refs");
}
if (faultPage.hasMore !== false || page.hasMore !== false) {
  throw new Error("transport recovery audit facts exceeded the verified event pages");
}
const faultMcp = (faultPage.events ?? []).find((event) => event.spec?.operation === "mcp.fail" &&
  event.spec?.resource === "McpServer" && event.spec?.changes?.length === 1 &&
  event.spec.changes[0]?.resource === "McpServer" && event.spec.changes[0]?.from === "" &&
  event.spec.changes[0]?.to === "failed" &&
  event.spec?.executionId === fault.metadata?.uid && event.spec?.turnId === fault.metadata?.turnId &&
  event.spec?.generation === fault.spec?.generation && event.spec?.serverId === mcpId &&
  event.spec?.version === process.env.CLOUD_AGENTS_COMPOSE_MCP_VERSION &&
  event.spec?.digest === process.env.CLOUD_AGENTS_COMPOSE_MCP_DIGEST &&
  event.spec?.result === "failed" && event.spec?.errorCode === "capability_call_unknown");
const reconnectMcp = (page.events ?? []).find((event) => event.spec?.operation === "mcp.call" &&
  event.spec?.resource === "McpServer" && event.spec?.changes?.length === 1 &&
  event.spec.changes[0]?.resource === "McpServer" && event.spec.changes[0]?.from === "" &&
  event.spec.changes[0]?.to === "succeeded" &&
  event.spec?.executionId === reconnect.metadata?.uid && event.spec?.turnId === reconnect.metadata?.turnId &&
  event.spec?.generation === reconnect.spec?.generation && event.spec?.serverId === mcpId &&
  event.spec?.version === process.env.CLOUD_AGENTS_COMPOSE_MCP_VERSION &&
  event.spec?.digest === process.env.CLOUD_AGENTS_COMPOSE_MCP_DIGEST &&
  event.spec?.result === "succeeded" && event.spec?.errorCode === undefined &&
  typeof event.spec?.resultDigest === "string");
if (!faultMcp || !reconnectMcp) throw new Error("transport recovery capability audit facts are incomplete");
const hasChange = (event, resource, from, to) => (event.spec?.changes ?? []).some((change) =>
  change.resource === resource && change.from === from && change.to === to);
const reconciled = (page.events ?? []).find((event) => event.spec?.operation === "execution.reconcile" &&
  event.spec?.resource === "Execution" && event.spec?.changes?.length === 1 &&
  event.spec?.executionId === cancelled.metadata?.uid && event.spec?.turnId === cancelled.metadata?.turnId &&
  event.spec?.generation === cancelled.spec?.generation &&
  hasChange(event, "Execution", "recovery:awaiting_reconciliation", "recovery:none"));
const cancelledEvent = (page.events ?? []).find((event) => event.spec?.operation === "turn.cancel" &&
  event.spec?.resource === "Execution" && event.spec?.changes?.length === 2 &&
  event.spec?.executionId === cancelled.metadata?.uid && event.spec?.turnId === cancelled.metadata?.turnId &&
  event.spec?.generation === cancelled.spec?.generation &&
  hasChange(event, "Turn", "running", "cancelled") &&
  hasChange(event, "Execution", "running", "cancelled"));
if (!reconciled || !cancelledEvent ||
    BigInt(faultMcp.metadata.sequence) >= BigInt(reconciled.metadata.sequence) ||
    BigInt(reconciled.metadata.sequence) >= BigInt(cancelledEvent.metadata.sequence) ||
    BigInt(cancelledEvent.metadata.sequence) >= BigInt(reconnectMcp.metadata.sequence)) {
  throw new Error("transport recovery lifecycle audit facts are incomplete");
}
const serialized = JSON.stringify([faultPage, page]);
const materialized = descriptor.mcp?.find((item) => item.resourceId === mcpId) ?? {};
for (const value of [
  process.env.CLOUD_AGENTS_COMPOSE_MARKER,
  `SDK_MCP_${process.env.CLOUD_AGENTS_COMPOSE_MARKER}`,
  materialized.endpoint,
  materialized.token,
  process.env.CLOUD_AGENTS_COMPOSE_FAULT_PROMPT,
  process.env.CLOUD_AGENTS_COMPOSE_RECONNECT_PROMPT,
].filter((value) => typeof value === "string" && value.length > 0)) {
  if (serialized.includes(value)) throw new Error("capability audit exposed sensitive transport data");
}
const forbiddenKeys = new Set([
  "prompt", "endpoint", "token", "secret", "toolinput", "tooloutput", "toolarguments", "toolresult",
  "requestbody", "responsebody", "mcpresult",
]);
const inspectKeys = (value) => {
  if (Array.isArray(value)) return value.forEach(inspectKeys);
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (forbiddenKeys.has(key.toLowerCase())) throw new Error(`capability audit exposed ${key}`);
    inspectKeys(child);
  }
};
inspectKeys(faultPage);
inspectKeys(page);
NODE
  echo "capability_transport_recovery=passed provider=$transport_provider_kind environment=$real_provider_environment_slug side_effects=1 fault_requests=1 replayed=0 reconnect=passed" >&2
}

run_real_provider_turn() {
  provider_kind=$1
  provider_slug=$2
  session_id="session-$real_provider_run_prefix-$provider_slug"
  turn_id="turn-$real_provider_run_prefix-$provider_slug"
  execution_id="execution-$real_provider_run_prefix-$provider_slug"
  artifact_path=".cloud-agents-stage3-acceptance-$real_provider_environment_slug-target-real-$provider_slug.txt"
  artifact_prompt_path=$artifact_path
  expected_content="cloud-agents $real_provider_environment_label target $provider_kind real E2E"
  case "$provider_kind" in
    codex) file_tool="You must use the workspace.write_text_file tool, never a shell command or another tool, to create" ;;
    claudeAgent)
      file_tool="You must use the Write tool, never a shell command or another tool, to create"
      artifact_prompt_path="/workspace/.cloud-agents/managed-agent/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id/workspace/$artifact_path"
      ;;
    pi | deepseek-harness) file_tool="You must use your file-writing tool, never a shell command, to create" ;;
  esac
  artifact_requirement="exactly one file at $artifact_prompt_path. Its complete contents must be the single ASCII line '$expected_content' followed by a newline"
  artifact_instruction="$file_tool $artifact_requirement"
  prompt="$artifact_instruction. Do not modify any other file. Then reply done."
  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    mcp_instruction="Call the managed MCP server cloud_agents_$capability_mcp_id tool acceptance_side_effect exactly once and wait for it to succeed."
    case "$provider_kind" in
      codex)
        skill_instruction='use the $managed-capability-acceptance Skill'
        mcp_instruction="Call the managed MCP tool named acceptance_side_effect exactly once and wait for it to succeed."
        file_tool="use the workspace.write_text_file tool exactly once to create"
        ;;
      claudeAgent) skill_instruction="use the managed-capability-acceptance:managed-capability-acceptance Skill" ;;
      *) skill_instruction="use the managed-capability-acceptance Skill" ;;
    esac
    artifact_instruction="$file_tool $artifact_requirement"
    prompt="$mcp_instruction Then $skill_instruction. Follow it to $artifact_instruction. Do not modify any other file or call any other tool. Only then reply done."
  fi

  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-session" --idempotency-key "$real_provider_run_prefix-$provider_slug-session" \
    session create --provider "$provider_kind" $capability_session_flags --workspace "$real_provider_workspace_id" \
    --sandbox "$real_provider_sandbox_id" --sandbox-generation "$real_provider_sandbox_generation" \
    --environment-profile "$real_provider_environment_profile_id" --environment-profile-version 1 >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-turn" --idempotency-key "$real_provider_run_prefix-$provider_slug-turn" \
    turn create --input "$prompt" >/dev/null
  execution_file="$smoke_directory/$execution_id.json"
  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    if ! execute_real_provider_with_mcp_approvals "$session_id" "$turn_id" "$execution_id" \
      "$real_provider_run_prefix-$provider_slug-execution" "$prompt" "$execution_file"; then
      return 1
    fi
    completed_real_provider_turn_id=$turn_id
    completed_real_provider_execution_id=$execution_id
  elif ! execute_real_provider_with_safe_retry "$provider_kind" "$session_id" "$turn_id" "$execution_id" \
      "$real_provider_run_prefix-$provider_slug-execution" "$prompt" "$execution_file"; then
      return 1
  fi
  turn_id=$completed_real_provider_turn_id
  execution_id=$completed_real_provider_execution_id

  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    sync_capability_mcp_fixture
    if ! CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$execution_file" \
      CLOUD_AGENTS_COMPOSE_MCP_ID="$capability_mcp_id" \
      CLOUD_AGENTS_COMPOSE_SKILL_ID="$capability_skill_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
const completed = (value.messages ?? []).filter((message) =>
  message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
  message.payload?.payload?.status === "completed");
const usedSkill = completed.some((message) =>
  message.payload?.payload?.itemType === "dynamic_tool_call" &&
  String(message.payload?.payload?.data?.sourceItemType ?? "").toLowerCase() === "skill" &&
  message.payload?.payload?.data?.capabilityResourceId === process.env.CLOUD_AGENTS_COMPOSE_SKILL_ID);
const calledMcp = completed.some((message) =>
  message.payload?.payload?.itemType === "mcp_tool_call" &&
  message.payload?.payload?.data?.capabilityResourceId === process.env.CLOUD_AGENTS_COMPOSE_MCP_ID);
const mcpRefs = value.spec?.mcpServerRefs ?? [];
const skillRefs = value.spec?.skillBundleRefs ?? [];
const refsMatch = mcpRefs.length === 1 && mcpRefs[0]?.serverId === process.env.CLOUD_AGENTS_COMPOSE_MCP_ID &&
  skillRefs.length === 1 && skillRefs[0]?.bundleId === process.env.CLOUD_AGENTS_COMPOSE_SKILL_ID;
if (value.spec?.state !== "succeeded" || !refsMatch || !usedSkill || !calledMcp) {
  console.error(JSON.stringify({
    messages: (value.messages ?? []).map((message) => ({
      messageType: message.messageType,
      eventType: message.payload?.eventType,
      itemType: message.payload?.payload?.itemType,
      status: message.payload?.payload?.status,
      sourceItemType: message.payload?.payload?.data?.sourceItemType,
      capabilityResourceId: message.payload?.payload?.data?.capabilityResourceId,
    })),
    refs: { mcp: mcpRefs, skill: skillRefs },
  }));
  process.exit(1);
}
NODE
    then
      echo "real $provider_kind execution did not complete the managed MCP and Skill" >&2
      if [ "$mcp_fixture_kubernetes" -eq 1 ]; then
        kubernetes_ctl -n "$kubernetes_active_namespace" exec "$mcp_fixture_pod" -c "$mcp_fixture_pod_container" -- \
          sh -c 'cat /tmp/cloud-agents-mcp-fixture.log' >&2 || true
      else
        docker inspect --format 'mcp-fixture state={{.State.Status}} exit={{.State.ExitCode}} error={{.State.Error}} network={{.HostConfig.NetworkMode}}' "$mcp_fixture_container" >&2 2>/dev/null || true
        docker logs --tail=80 "$mcp_fixture_container" >&2 2>/dev/null || true
      fi
      if [ -f "$smoke_directory/mcp-side-effect/requests.log" ]; then
        sed -n '1,80p' "$smoke_directory/mcp-side-effect/requests.log" >&2
      fi
      return 1
    fi
    if [ "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')" -ne 1 ] || \
      ! grep -Fqx "$mcp_fixture_marker" "$mcp_fixture_side_effect_file"; then
      echo "real $provider_kind execution did not record exactly one managed MCP side effect" >&2
      return 1
    fi
    mcp_request_count=$(wc -l <"$smoke_directory/mcp-side-effect/requests.log" 2>/dev/null || printf '0')
  fi

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
  const rejectionPattern = /^Generated-file candidates rejected: (?:(?:missing_path|invalid_path|outside_workspace|missing|symlink|not_regular)=[0-9]{1,3}(?:, |$))+$/;
  const rejections = value.messages.flatMap((message) => {
    const text = message.payload?.payload?.message;
    return message.payload?.eventType === "runtime.warning" && typeof text === "string" &&
      text.length <= 256 && rejectionPattern.test(text) ? [text] : [];
  });
  console.error(JSON.stringify({ generatedFileRejections: rejections }));
  console.error(JSON.stringify(value.messages.map((message) => ({
    messageType: message.messageType,
    eventType: message.payload?.eventType,
    itemType: message.payload?.payload?.itemType,
    status: message.payload?.payload?.status,
    failureKind: message.payload?.payload?.failureKind,
    terminalEventType: message.payload?.payload?.terminalEventType,
    errorCode: message.payload?.payload?.errorCode,
    sourceItemType: message.payload?.payload?.data?.sourceItemType,
    capabilityResourceId: message.payload?.payload?.data?.capabilityResourceId,
    artifactKind: message.payload?.artifact?.kind,
    artifactPath: message.payload?.artifact?.path,
    artifactSourceRoot: message.payload?.artifact?.sourceRoot,
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

  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    echo "capability_acceptance=passed provider=$provider_kind environment=$real_provider_environment_slug mcp_requests=$mcp_request_count side_effects=1 skill=1 artifact=verified events=resumed" >&2
  fi

  followup_turn_id="$turn_id-followup"
  followup_execution_id="$execution_id-followup"
  followup_expected_command=
  if [ "$provider_kind" = codex ]; then
    followup_expected_command="/bin/bash -lc 'cat -- $artifact_path'"
    followup_prompt="Run exactly this command and no other tool: cat -- $artifact_path. Reply with the command's exact single line. Do not modify any file."
  else
    followup_prompt="Read $artifact_prompt_path and reply with its exact single line. Do not modify any file."
  fi
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$followup_turn_id" \
    --request-id "$real_provider_run_prefix-$provider_slug-followup-turn" --idempotency-key "$real_provider_run_prefix-$provider_slug-followup-turn" \
    turn create --input "$followup_prompt" >/dev/null
  followup_file="$smoke_directory/$followup_execution_id.json"
  if ! execute_real_provider_with_safe_retry "$provider_kind" "$session_id" "$followup_turn_id" "$followup_execution_id" \
    "$real_provider_run_prefix-$provider_slug-followup-execution" "$followup_prompt" "$followup_file" \
    "$followup_expected_command"; then
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

  if [ "$capability_acceptance" -eq 1 ] && [ "$capability_bound_recovery" -eq 1 ] && capability_provider_enabled "$provider_kind" &&
    ! capability_bound_recovery_completed "$real_provider_environment_slug" "$provider_kind" &&
    { [ -n "$mcp_fixture_container" ] || [ -n "$mcp_fixture_pod" ]; }; then
    case "$real_provider_environment_slug" in
      docker) capability_recovery_target=docker-compose-target ;;
      remote-worker) capability_recovery_target=$remote_target_id ;;
      kubernetes) capability_recovery_target=$kubernetes_runtime_target_id ;;
    esac
    run_real_provider_recovery "$provider_kind" "$provider_slug" \
      "$real_provider_environment_slug" "$real_provider_environment_label" \
      "$real_provider_workspace_id" "$real_provider_sandbox_id" "$real_provider_sandbox_generation" \
      "$real_provider_environment_profile_id" "$capability_recovery_target" 0
    capability_bound_recoveries="$capability_bound_recoveries $real_provider_environment_slug:$provider_kind"
    echo "capability_bound_recovery=passed provider=$provider_kind environment=$real_provider_environment_slug mode=process-restart" >&2
  fi

  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    run_capability_transport_recovery "$provider_kind" "$provider_slug"
  fi

  capability_revoke_here=0
  if [ "$capability_acceptance" -eq 1 ] && capability_provider_enabled "$provider_kind"; then
    case "$real_provider_environment_slug:$remote_runtime:$kubernetes_runtime" in
      docker:0:0 | remote-worker:1:0 | kubernetes:*:1) capability_revoke_here=1 ;;
    esac
  fi
  if [ "$capability_revoke_here" -eq 1 ]; then
    capability_mcp_revoke_body='{"expectedResourceVersion":"1","reasonCode":"acceptance-complete"}'
    capability_skill_revoke_body='{"expectedResourceVersion":"1","reasonCode":"acceptance-complete"}'
    control_plane_api "$smoke_directory/admin-curl.conf" POST \
      "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/mcp-servers/$capability_mcp_id:revoke" \
      compose-smoke-capability-mcp-revoke --header "Idempotency-Key: compose-smoke-capability-mcp-revoke" \
      --data "$capability_mcp_revoke_body" >"$smoke_directory/capability-mcp-revoked.json"
    control_plane_api "$smoke_directory/admin-curl.conf" POST \
      "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/skill-bundles/$capability_skill_id:revoke" \
      compose-smoke-capability-skill-revoke --header "Idempotency-Key: compose-smoke-capability-skill-revoke" \
      --data "$capability_skill_revoke_body" >"$smoke_directory/capability-skill-revoked.json"
    CAPABILITY_MCP_FILE="$smoke_directory/capability-mcp-revoked.json" \
    CAPABILITY_SKILL_FILE="$smoke_directory/capability-skill-revoked.json" node <<'NODE'
const { readFileSync } = require("node:fs");
for (const path of [process.env.CAPABILITY_MCP_FILE, process.env.CAPABILITY_SKILL_FILE]) {
  const value = JSON.parse(readFileSync(path, "utf8"));
  if (value.spec?.status !== "revoked" || value.metadata?.resourceVersion !== "2" || typeof value.spec?.revokedAt !== "string") {
    throw new Error("capability catalog did not persist revocation");
  }
}
NODE

    sync_capability_mcp_fixture
    capability_request_count_before_revoke=$(wc -l <"$smoke_directory/mcp-side-effect/requests.log" 2>/dev/null || printf '0')
    capability_side_effect_count_before_revoke=$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')
    for revoked_capability_kind in mcp skill; do
      case "$revoked_capability_kind" in
        mcp)
          revoked_capability_flags="--mcp-server-refs-json $capability_mcp_refs_json"
          revoked_capability_operation=mcp.revoke
          revoked_capability_resource=$capability_mcp_id
          ;;
        skill)
          revoked_capability_flags="--skill-bundle-refs-json $capability_skill_refs_json"
          revoked_capability_operation=skill.revoke
          revoked_capability_resource=$capability_skill_id
          ;;
      esac
      revoked_session_id="session-$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind"
      revoked_turn_id="turn-$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind"
      revoked_execution_id="execution-$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind"
      cloud_agentsctl_user --project "$project_id" --session "$revoked_session_id" \
        --request-id "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-session" \
        --idempotency-key "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-session" \
        session create --provider "$provider_kind" --workspace "$real_provider_workspace_id" \
        --sandbox "$real_provider_sandbox_id" --sandbox-generation "$real_provider_sandbox_generation" \
        --environment-profile "$real_provider_environment_profile_id" --environment-profile-version 1 >/dev/null
      cloud_agentsctl_user --project "$project_id" --session "$revoked_session_id" --turn "$revoked_turn_id" \
        --request-id "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-turn" \
        --idempotency-key "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-turn" \
        turn create --input "This execution must fail before Runtime opens." >/dev/null
      revoked_execution_file="$smoke_directory/$revoked_execution_id.json"
      revoked_execution_error="$smoke_directory/$revoked_execution_id.err"
      if cloud_agentsctl_user --project "$project_id" --session "$revoked_session_id" --turn "$revoked_turn_id" \
        --execution "$revoked_execution_id" --request-id "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-execution" \
        --idempotency-key "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-execution" \
        execution execute --runtime-mode full-access --interaction-mode default $revoked_capability_flags \
        --input "This execution must fail before Runtime opens." >"$revoked_execution_file" 2>"$revoked_execution_error"; then
        echo "Compose accepted revoked $revoked_capability_kind capability" >&2
        exit 1
      fi
      if ! grep -Fq 'CAPABILITY_UNAVAILABLE' "$revoked_execution_error"; then
        cat "$revoked_execution_error" >&2
        echo "Compose revoked $revoked_capability_kind did not return capability_unavailable" >&2
        exit 1
      fi
      revoked_events_file="$smoke_directory/$revoked_execution_id-events.json"
      cloud_agentsctl_user --project "$project_id" --session "$revoked_session_id" \
        --request-id "$real_provider_run_prefix-$provider_slug-revoked-$revoked_capability_kind-events" \
        events list --limit 64 >"$revoked_events_file"
      CLOUD_AGENTS_COMPOSE_EVENTS_FILE="$revoked_events_file" \
      CLOUD_AGENTS_COMPOSE_OPERATION="$revoked_capability_operation" \
      CLOUD_AGENTS_COMPOSE_RESOURCE_ID="$revoked_capability_resource" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EVENTS_FILE, "utf8"));
const event = value.events?.find((item) => item.spec?.operation === process.env.CLOUD_AGENTS_COMPOSE_OPERATION);
const resourceId = event?.spec?.serverId ?? event?.spec?.bundleId;
if (!event || resourceId !== process.env.CLOUD_AGENTS_COMPOSE_RESOURCE_ID ||
    event.spec?.result !== "revoked" || event.spec?.errorCode !== "capability_revoked") {
  console.error(JSON.stringify((value.events ?? []).map((item) => ({
    operation: item.spec?.operation,
    resource: item.spec?.resource,
    result: item.spec?.result,
    errorCode: item.spec?.errorCode,
    serverId: item.spec?.serverId,
    bundleId: item.spec?.bundleId,
  }))));
  throw new Error("revoked capability did not produce the expected opaque audit event");
}
NODE
    done
    sync_capability_mcp_fixture
    test "$(wc -l <"$smoke_directory/mcp-side-effect/requests.log" 2>/dev/null || printf '0')" -eq "$capability_request_count_before_revoke"
    test "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf '0')" -eq "$capability_side_effect_count_before_revoke"
    echo "capability_revocation_negative=passed provider=$provider_kind environment=$real_provider_environment_slug runtime_requests=0 side_effects=0 events=2" >&2
  fi
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
const terminalIndex = baseline.events.findIndex((event) =>
  event.spec?.operation === "execution.complete" && event.spec?.executionId === process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_ID);
if (terminalIndex < 1) throw new Error("real Provider event baseline is missing execution.complete");
const expected = baseline.events.slice(1, terminalIndex + 1).map((event) => event.metadata.uid);
const actual = resumed.map((event) => event.metadata.uid);
if (interrupted.length === 0 || interrupted[0].metadata.uid !== expected[0]) throw new Error("event watch disconnect did not start at the persisted cursor");
if (actual.length !== expected.length || actual.some((uid, index) => uid !== expected[index])) {
  console.error(JSON.stringify({
    expected,
    actual,
    baseline: baseline.events.map((event) => ({uid: event.metadata?.uid, operation: event.spec?.operation, eventType: event.spec?.eventType})),
    resumed: resumed.map((event) => ({uid: event.metadata?.uid, operation: event.spec?.operation, eventType: event.spec?.eventType})),
    postTerminal: baseline.events.slice(terminalIndex + 1).map((event) => ({uid: event.metadata?.uid, operation: event.spec?.operation, eventType: event.spec?.eventType})),
  }));
  throw new Error("event watch resume skipped or duplicated persisted events");
}
const last = resumed.at(-1);
if (last?.spec?.operation !== "execution.complete" || last.spec.executionId !== process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_ID) throw new Error("event watch resume did not reach execution.complete");
process.stdout.write(`event_stream_resume=passed interrupted=${interrupted[0].metadata.uid} resumed_events=${resumed.length} post_terminal_events=${baseline.events.length - terminalIndex - 1}\n`);
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
  recovery_snapshot_digest=$(read_recovery_snapshot_digest)
  printf '%s\n' "$recovery_snapshot_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
    echo "Codex recovery snapshot content digest is missing or invalid" >&2
    exit 1
  }
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
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
    foundation_agent_runtime_id=${foundation_agent_error%%|*}
    remember_opensandbox_runtime_id "$foundation_agent_runtime_id"
    foundation_agent_error=${foundation_agent_error#*|}
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
  recovery_snapshot_digest=$(read_recovery_snapshot_digest)
  printf '%s\n' "$recovery_snapshot_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
    echo "Kubernetes recovery snapshot content digest is missing or invalid" >&2
    exit 1
  }
  if [ "$recovery_cross_node" -eq 1 ] && [ "$recovery_environment_slug" = kubernetes ]; then
    recovery_snapshot_archive_id=$(CLOUD_AGENTS_COMPOSE_PROJECT="$project_id" \
      CLOUD_AGENTS_COMPOSE_WORKSPACE="$source_workspace_id" \
      CLOUD_AGENTS_COMPOSE_SNAPSHOT="$recovery_snapshot_id" node <<'NODE'
const { createHash } = require("node:crypto");
const values = ["tenant-compose-smoke", process.env.CLOUD_AGENTS_COMPOSE_PROJECT,
  process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE, process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT,
  "portable-archive"];
const hash = createHash("sha256");
for (const value of values) hash.update(value).update(Buffer.from([0]));
process.stdout.write(`ca-portable-snapshot-${hash.digest("hex").slice(0, 64 - "ca-portable-snapshot-".length)}`);
NODE
    )
    recovery_snapshot_archive_path="$smoke_directory/snapshots/$recovery_snapshot_archive_id.tar"
    if [ ! -f "$recovery_snapshot_archive_path" ]; then
      echo "Kubernetes recovery snapshot raw archive was not found at its deterministic archive path" >&2
      exit 1
    fi
    recovery_snapshot_archive_size=$(wc -c <"$recovery_snapshot_archive_path" | tr -d ' ')
    if [ "$recovery_snapshot_archive_size" != "$recovery_snapshot_size" ]; then
      echo "Kubernetes recovery snapshot raw archive size did not match the API size" >&2
      exit 1
    fi
    recovery_snapshot_raw_sha256=$(sha256sum "$recovery_snapshot_archive_path" | awk '{print $1}')
    if [ -n "$snapshot_archive_output" ]; then
      mkdir -p "$(dirname "$snapshot_archive_output")"
      cp "$recovery_snapshot_archive_path" "$snapshot_archive_output"
      chmod 0600 "$snapshot_archive_output"
      printf 'MCP_SKILL_RUNTIME_V1_SNAPSHOT_ARCHIVE=%s sha256=%s size_bytes=%s\n' \
        "$snapshot_archive_output" "$recovery_snapshot_raw_sha256" "$recovery_snapshot_size"
    fi
  fi
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
    kubernetes_agent_state=${kubernetes_agent_values%%|*}
    kubernetes_agent_rest=${kubernetes_agent_values#*|}
    kubernetes_agent_target=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_writer_released=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_generation=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_resource_version=${kubernetes_agent_rest%%|*}
    kubernetes_agent_rest=${kubernetes_agent_rest#*|}
    kubernetes_agent_runtime_id=${kubernetes_agent_rest%%|*}
    remember_opensandbox_runtime_id "$kubernetes_agent_runtime_id"
    kubernetes_agent_error=${kubernetes_agent_rest#*|}
    if [ "$kubernetes_destination_volume_bound" -eq 0 ]; then
      kubernetes_agent_volume=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-recovery-sandbox-restored.json" node -e \
        'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(value.spec?.physicalVolumeId??"")')
      if [ -n "$kubernetes_agent_volume" ]; then
        kubernetes_bind_workspace_volume "$kubernetes_destination_namespace" "$kubernetes_agent_volume" "$kubernetes_destination_node"
        kubernetes_recovery_bound_pod=ca-workspace-binder
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

capture_kubernetes_restored_snapshot_digest() {
  recovery_restored_archive_path="$smoke_directory/$recovery_prefix-restored-workspace.tar"
  kubernetes_ctl -n "$kubernetes_destination_namespace" exec "$kubernetes_recovery_bound_pod" -c binder -- \
    tar -cf - -C /workspace . >"$recovery_restored_archive_path"
  recovery_restored_probe_values=$(node "$script_directory/lib/portable-snapshot-digest.mjs" \
    "$recovery_restored_archive_path" | tr '\n' '|' | sed 's/|$//')
  recovery_restored_content_digest=${recovery_restored_probe_values%%|*}
  recovery_restored_snapshot_sha256=${recovery_restored_probe_values#*|}
  printf '%s\n' "$recovery_restored_content_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
    echo "Kubernetes restored Workspace/Sandbox semantic digest is missing or invalid" >&2
    exit 1
  }
  printf '%s\n' "$recovery_restored_snapshot_sha256" | grep -Eq '^[0-9a-f]{64}$' || {
    echo "Kubernetes restored Workspace/Sandbox archive digest is missing or invalid" >&2
    exit 1
  }
  recovery_restored_content_digest_sha256=${recovery_restored_content_digest#sha256:}
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
  recovery_snapshot_digest=$(read_recovery_snapshot_digest)
  printf '%s\n' "$recovery_snapshot_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
    echo "RemoteWorker recovery snapshot content digest is missing or invalid" >&2
    exit 1
  }
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write([value.spec?.observedState,value.spec?.targetId,value.spec?.writerReleased,value.spec?.generation,value.metadata?.resourceVersion,value.spec?.runtimeId??"",value.spec?.stableErrorCode??""].join("|"))')
    remote_agent_state=${remote_agent_values%%|*}; remote_agent_rest=${remote_agent_values#*|}
    remote_agent_target=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_writer_released=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_generation=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_resource_version=${remote_agent_rest%%|*}; remote_agent_rest=${remote_agent_rest#*|}
    remote_agent_runtime_id=${remote_agent_rest%%|*}; remote_agent_error=${remote_agent_rest#*|}
    remember_opensandbox_runtime_id "$remote_agent_runtime_id"
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
  recovery_capability_bound=0
  recovery_session_flags=
  recovery_execution_flags=
  if [ "$capability_acceptance" -eq 1 ] &&
    { [ -n "$mcp_fixture_container" ] || [ -n "$mcp_fixture_pod" ]; }; then
    case "$recovery_provider_kind" in
      codex | claudeAgent | pi | deepseek-harness)
        recovery_capability_bound=1
        recovery_session_flags="$capability_session_flags"
        recovery_execution_flags="$capability_execution_flags"
        ;;
    esac
  fi
  if [ "$recovery_capability_bound" -eq 1 ]; then
    recovery_prefix="$recovery_prefix-capability"
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
      recovery_artifact_directory=${artifact_path%/*}
      recovery_shell_command="mkdir -p '$recovery_artifact_directory' && printf '%s\\n' '$expected_content' > '$artifact_path' && sleep 12"
      recovery_file_instruction="use the workspace.write_text_file tool exactly once with path '$artifact_path' and content '$expected_content\\n'"
      prompt="${recovery_file_instruction}. Do not use a shell command. Then reply done. Do not reply done unless the managed tool succeeds."
      ;;
    claudeAgent | pi)
      recovery_artifact_directory=${artifact_path%/*}
      recovery_shell_command="mkdir -p '$recovery_artifact_directory' && printf '%s\\n' '$expected_content' > '$artifact_path' && sleep 12"
      recovery_file_instruction="use the Bash tool exactly once to run this exact command: $recovery_shell_command"
      prompt="${recovery_file_instruction}. Do not use another tool. Then reply done. Do not reply done unless the tool succeeds."
      ;;
    deepseek-harness)
      recovery_artifact_directory=${artifact_path%/*}
      recovery_shell_command="mkdir -p '$recovery_artifact_directory' && printf '%s\\n' '$expected_content' > '$artifact_path' && sleep 12"
      recovery_file_instruction="use the str_replace_editor tool exactly once with command=create, path='$recovery_artifact_absolute', and file_text='$expected_content\\n'"
      prompt="${recovery_file_instruction}. Do not use a shell command or another tool. Then reply done. Do not reply done unless the tool succeeds."
      ;;
  esac
  if [ "$recovery_capability_bound" -eq 1 ]; then
    recovery_mcp_tool_name=acceptance_marker
    recovery_skill_name=managed-capability-acceptance
    if [ "$recovery_provider_kind" = "claudeAgent" ]; then
      recovery_skill_name=managed-capability-acceptance:managed-capability-acceptance
    fi
    if [ "$recovery_provider_kind" = "pi" ]; then
      recovery_mcp_tool_name=mcp__mcp-compose-acce__acceptance_marker__a63aebad
    fi
    if [ "$recovery_provider_kind" = "codex" ]; then
      prompt="Call the managed MCP tool named $recovery_mcp_tool_name exactly once, then use the $recovery_skill_name Skill for this managed capability recovery request. The Codex Host-managed workspace.write_text_file tool is the only available file tool; follow the Skill's recovery intent with $recovery_file_instruction. Do not use a shell command or another tool. Then reply done. Do not reply done unless the managed tool succeeds."
    elif [ "$recovery_provider_kind" = "deepseek-harness" ]; then
      prompt="$recovery_file_instruction first. Then call the managed MCP tool named $recovery_mcp_tool_name exactly once and use the $recovery_skill_name Skill for this managed capability recovery request. Do not use a shell command or another tool. Then reply done. Do not reply done unless every managed tool succeeds."
    else
      prompt="Call the managed MCP tool named $recovery_mcp_tool_name exactly once, then use the $recovery_skill_name Skill for this managed capability recovery request. Follow it exactly: $recovery_file_instruction. Do not use another tool. Then reply done. Do not reply done unless the tool succeeds."
    fi
  fi
  recovery_runtime_mode=full-access
  if [ "$recovery_capability_bound" -eq 1 ] && [ "$recovery_provider_kind" = "codex" ]; then
    recovery_runtime_mode=approval-required
  fi
  cross_recovery_rto_ms=0
  recovery_snapshot_size=0
  recovery_snapshot_digest=
  recovery_snapshot_archive_path=
  recovery_snapshot_raw_sha256=

  cloud_agentsctl_user --project "$project_id" --session "$session_id" \
    --request-id "$recovery_prefix-session" --idempotency-key "$recovery_prefix-session" \
    session create --provider "$recovery_provider_kind" $recovery_session_flags --workspace "$recovery_workspace_id" \
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
      --runtime-mode "$recovery_runtime_mode" --interaction-mode default $recovery_execution_flags --input "$prompt" \
      >"$recovery_execute_file" 2>"$recovery_execute_error"
    printf '%s\n' "$?" >"$recovery_execute_status"
  ) &
  recovery_execute_pid=$!

  recovery_checkpoint_file="$smoke_directory/$recovery_prefix-checkpoint.json"
  recovery_handled_approvals=
  recovery_checkpoint_poll_sleep=1
  recovery_checkpoint_attempt_limit=120
  if [ "$recovery_provider_kind" = deepseek-harness ]; then
    recovery_checkpoint_poll_sleep=0.05
    recovery_checkpoint_attempt_limit=1200
  fi
  attempt=0
  while :; do
    if [ -f "$recovery_execute_status" ]; then
      cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
        --execution "$execution_id" --request-id "$recovery_prefix-failure" execution get \
        >"$recovery_checkpoint_file" 2>/dev/null || true
      CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file" \
        CLOUD_AGENTS_COMPOSE_EXECUTION_ERROR_FILE="$recovery_execute_error" node <<'NODE'
const { existsSync, readFileSync } = require("node:fs");
const { createHash } = require("node:crypto");
const readJSON = (path) => { try { return JSON.parse(readFileSync(path, "utf8")); } catch { return {}; } };
const value = readJSON(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE);
const errorText = existsSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_ERROR_FILE)
  ? readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_ERROR_FILE, "utf8") : "";
const safe = (value) => typeof value === "string" && /^[a-z][a-z0-9_.-]{0,79}$/.test(value) ? value : undefined;
console.error(JSON.stringify({ capabilityRecoveryFailure: {
  state: safe(value.spec?.state), errorCode: safe(value.spec?.errorCode),
  errorDigests: [value.spec?.errorMessage, errorText].filter((text) => typeof text === "string" && text)
    .map((text) => createHash("sha256").update(text).digest("hex")),
  messages: (value.messages ?? []).map((message) => ({
    type: safe(message.messageType?.toLowerCase()), event: safe(message.payload?.eventType),
    errorCode: safe(message.error?.code), status: safe(message.payload?.payload?.status),
  })),
} }));
NODE
      echo "$recovery_provider_kind $recovery_environment_label recovery Turn ended before a pending-side-effect checkpoint" >&2
      exit 1
    fi
    if cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
      --execution "$execution_id" --request-id "$recovery_prefix-checkpoint" \
      execution get >"$recovery_checkpoint_file" 2>/dev/null; then
      recovery_pending_info=$(CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file" \
        CLOUD_AGENTS_COMPOSE_HANDLED_APPROVALS="$recovery_handled_approvals" \
        CLOUD_AGENTS_COMPOSE_EXPECTED_COMMAND="$recovery_shell_command" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
const handled = new Set((process.env.CLOUD_AGENTS_COMPOSE_HANDLED_APPROVALS ?? "").split(" ").filter(Boolean));
const expectedCommand = process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_COMMAND;
const normalizeShellCommand = (command) => typeof command === "string"
  ? command.replace(/^\/bin\/bash -l?c /, "/bin/bash -c ") : command;
for (const message of value.messages ?? []) {
  if (message.messageType !== "InteractionRequest" || message.payload?.interactionType !== "approval") continue;
  const requestId = message.payload?.requestId;
  const categories = message.payload?.sensitiveAction?.categories;
  const isMcpApproval = Array.isArray(categories) && categories.includes("external-mcp-action");
  const isExpectedCodexCommand = expectedCommand && message.payload?.provider === "codex" &&
    message.payload?.requestKind === "command" &&
    normalizeShellCommand(message.payload?.command) === normalizeShellCommand(expectedCommand);
  if (typeof requestId === "string" && !handled.has(requestId) &&
      (isMcpApproval || isExpectedCodexCommand) && Number.isSafeInteger(value.spec?.generation)) {
    process.stdout.write(`approval|${value.spec.generation}|${requestId}`);
    process.exit(0);
  }
}
if (value.spec?.state === "running" && value.spec?.checkpoint?.pendingSideEffect === true) process.stdout.write("pending");
NODE
      ) || true
      case "$recovery_pending_info" in
        pending) break ;;
        approval\|*)
          recovery_approval_rest=${recovery_pending_info#approval|}
          recovery_approval_generation=${recovery_approval_rest%%|*}
          recovery_approval_request=${recovery_approval_rest#*|}
          cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
            --execution "$execution_id" --request-id "$recovery_prefix-approve-$attempt" \
            --idempotency-key "$recovery_prefix-approve-$attempt" execution resolve-approval \
            --generation "$recovery_approval_generation" --interaction-request "$recovery_approval_request" --decision accept >/dev/null
          recovery_handled_approvals="$recovery_handled_approvals $recovery_approval_request"
          ;;
      esac
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$recovery_checkpoint_attempt_limit" ]; then
      echo "$recovery_provider_kind $recovery_environment_label recovery Turn did not persist a pending-side-effect checkpoint" >&2
      exit 1
    fi
    sleep "$recovery_checkpoint_poll_sleep"
  done

  if [ "$recovery_environment_slug" != remote-worker ]; then
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
  fi
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
      capture_kubernetes_restored_snapshot_digest
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
    if [ "$recovery_capability_bound" -eq 1 ]; then
      case "$recovery_environment_slug" in
        kubernetes) start_capability_mcp_fixture "$kubernetes_agent_runtime_id" ;;
        remote-worker)
          if [ "$recovery_cross_node" -eq 1 ]; then
            start_capability_mcp_fixture "$remote_agent_runtime_id" "tcp://127.0.0.1:$destination_docker_port"
          else
            start_capability_mcp_fixture "$remote_agent_runtime_id"
          fi
          ;;
        docker) start_capability_mcp_fixture "$foundation_agent_runtime_id" ;;
      esac
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
    --execution "$execution_id" --request-id "$recovery_prefix-blocked-execution" \
    --idempotency-key "$recovery_prefix-blocked-execution" execution execute \
    --runtime-mode "$recovery_runtime_mode" --interaction-mode default $recovery_execution_flags --input "$prompt" 2>&1)
  recovery_blocked_status=$?
  set -e
  if [ "$recovery_blocked_status" -eq 0 ]; then
    echo "$recovery_provider_kind $recovery_environment_label recovery replay bypassed side-effect reconciliation" >&2
    exit 1
  fi
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-blocked" \
    execution get >"$recovery_checkpoint_file"
  set +e
  recovery_claim_output=$(cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-claim" \
    --idempotency-key "$recovery_prefix-execution" execution execute \
    --runtime-mode "$recovery_runtime_mode" --interaction-mode default $recovery_execution_flags --input "$prompt" 2>&1)
  recovery_claim_status=$?
  set -e
  if [ "$recovery_claim_status" -eq 0 ] || ! printf '%s' "$recovery_claim_output" | grep -Fq 'RECOVERY_REQUIRES_RECONCILIATION'; then
    echo "$recovery_provider_kind $recovery_environment_label recovery claim did not fail closed for reconciliation" >&2
    exit 1
  fi
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_prefix-reconcile-state" \
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
  recovery_execute_prefix="$recovery_prefix-recovery-execution"
  if [ "$recovery_capability_bound" -eq 1 ]; then
    if ! execute_real_provider_with_mcp_approvals "$session_id" "$turn_id" "$execution_id" \
      "$recovery_execute_prefix" "$prompt" "$recovery_result_file" "$recovery_shell_command" "$recovery_runtime_mode" \
      "$recovery_prefix-execution"; then
      cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
        --execution "$execution_id" --request-id "$recovery_prefix-failure" execution get >&2 || true
      exit 1
    fi
  elif ! cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "$recovery_execute_prefix" \
      --idempotency-key "$recovery_prefix-execution" execution execute \
      --runtime-mode "$recovery_runtime_mode" --interaction-mode default $recovery_execution_flags --input "$prompt" >"$recovery_result_file"; then
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
  CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND="$recovery_capability_bound" \
  CLOUD_AGENTS_COMPOSE_MCP_ID="$capability_mcp_id" \
  CLOUD_AGENTS_COMPOSE_SKILL_ID="$capability_skill_id" \
  CLOUD_AGENTS_COMPOSE_CROSS_RTO_MS="$cross_recovery_rto_ms" \
  CLOUD_AGENTS_COMPOSE_SNAPSHOT_SIZE="$recovery_snapshot_size" \
  CLOUD_AGENTS_COMPOSE_SNAPSHOT_DIGEST="$recovery_snapshot_digest" node <<'NODE'
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
if (process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND === "1") {
  const completed = (value.messages ?? []).filter((message) =>
    message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
    message.payload?.payload?.status === "completed");
  const calledMcp = completed.some((message) =>
    message.payload?.payload?.itemType === "mcp_tool_call" &&
    message.payload?.payload?.data?.capabilityResourceId === process.env.CLOUD_AGENTS_COMPOSE_MCP_ID);
  const usedSkill = completed.some((message) =>
    ["skill", "Skill"].includes(message.payload?.payload?.data?.sourceItemType) &&
    message.payload?.payload?.data?.capabilityResourceId === process.env.CLOUD_AGENTS_COMPOSE_SKILL_ID);
  const mcpRefs = value.spec?.mcpServerRefs ?? [];
  const skillRefs = value.spec?.skillBundleRefs ?? [];
  if (!calledMcp || !usedSkill || mcpRefs.length !== 1 || mcpRefs[0]?.serverId !== process.env.CLOUD_AGENTS_COMPOSE_MCP_ID ||
      skillRefs.length !== 1 || skillRefs[0]?.bundleId !== process.env.CLOUD_AGENTS_COMPOSE_SKILL_ID) {
    console.error(JSON.stringify({
      refs: {mcp: mcpRefs, skill: skillRefs},
      completed: completed.map((message) => message.payload?.payload?.data),
    }));
    throw new Error("capability-bound recovery did not retain MCP/Skill refs and usage across process restart");
  }
}
process.stdout.write(`ANYWHERE_RUNTIME_R4_RECOVERY=${JSON.stringify({
  provider: process.env.CLOUD_AGENTS_COMPOSE_PROVIDER,
  environment: process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT,
  capabilityBound: process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND === "1",
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
  snapshotDigest: process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_DIGEST || undefined,
})}\n`);
NODE
  recovery_final_probe=$(run_recovery_sandbox_probe final-probe \
    "sha256sum '$recovery_artifact_absolute' | cut -d' ' -f1")
  CLOUD_AGENTS_COMPOSE_EXECUTION="$recovery_final_probe" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_DIGEST="$expected_recovery_digest" node -e \
    'const value=JSON.parse(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION);if(value.exitCode!==0||value.stdout.trim()!==process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_DIGEST)process.exit(1)'
  if [ "$recovery_cross_node" -eq 1 ] && [ "$recovery_environment_slug" = kubernetes ]; then
    [ -n "${recovery_restored_content_digest_sha256:-}" ] || {
      echo "Kubernetes restored Workspace/Sandbox digest was not captured before replay" >&2
      exit 1
    }
    recovery_snapshot_pvc_json=$(kubernetes_ctl -n "$kubernetes_destination_namespace" get pvc "$kubernetes_agent_volume" -o json)
    recovery_snapshot_pv=$(printf '%s' "$recovery_snapshot_pvc_json" | node -e \
      'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.spec?.volumeName??"")')
    recovery_snapshot_pod=$kubernetes_recovery_bound_pod
    [ -n "$recovery_snapshot_pv" ] && [ -n "$recovery_snapshot_pod" ] || {
      echo "Kubernetes recovery evidence is missing the destination PV or Pod" >&2
      exit 1
    }
    if [ -n "$cross_node_evidence_file" ]; then
      CLOUD_AGENTS_COMPOSE_EVIDENCE_FILE="$cross_node_evidence_file" \
      CLOUD_AGENTS_COMPOSE_PROVIDER="$recovery_provider_kind" \
      CLOUD_AGENTS_COMPOSE_ENVIRONMENT="$recovery_environment_slug" \
      CLOUD_AGENTS_COMPOSE_RAW_SHA256="$recovery_snapshot_raw_sha256" \
      CLOUD_AGENTS_COMPOSE_CONTENT_DIGEST="$recovery_snapshot_digest" \
      CLOUD_AGENTS_COMPOSE_RESTORED_SHA256="$recovery_restored_content_digest_sha256" \
      CLOUD_AGENTS_COMPOSE_RESTORED_ARCHIVE_SHA256="$recovery_restored_snapshot_sha256" \
      CLOUD_AGENTS_COMPOSE_SNAPSHOT_SIZE="$recovery_snapshot_size" \
      CLOUD_AGENTS_COMPOSE_SOURCE_NODE="$kubernetes_source_node" \
      CLOUD_AGENTS_COMPOSE_DESTINATION_NODE="$kubernetes_destination_node" \
      CLOUD_AGENTS_COMPOSE_PVC="$kubernetes_agent_volume" \
      CLOUD_AGENTS_COMPOSE_PV="$recovery_snapshot_pv" \
      CLOUD_AGENTS_COMPOSE_POD="$recovery_snapshot_pod" \
      CLOUD_AGENTS_COMPOSE_POD_NODE="$kubernetes_destination_node" \
      CLOUD_AGENTS_COMPOSE_WORKSPACE="$recovery_workspace_id" \
      CLOUD_AGENTS_COMPOSE_SANDBOX="$recovery_sandbox_id" \
      CLOUD_AGENTS_COMPOSE_GENERATION="$recovery_sandbox_generation" \
      CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_result_file" \
      CLOUD_AGENTS_COMPOSE_CROSS_RTO_MS="$cross_recovery_rto_ms" \
      node <<'NODE'
const { mkdirSync, readFileSync, writeFileSync } = require("node:fs");
const path = process.env.CLOUD_AGENTS_COMPOSE_EVIDENCE_FILE;
mkdirSync(require("node:path").dirname(path), { recursive: true });
const execution = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
const evidence = {
  schema: "mcp-skill-runtime-v1/cross-node-snapshot-evidence-v1",
  provider: process.env.CLOUD_AGENTS_COMPOSE_PROVIDER,
  environment: process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT,
  sourceNode: process.env.CLOUD_AGENTS_COMPOSE_SOURCE_NODE,
  destinationNode: process.env.CLOUD_AGENTS_COMPOSE_DESTINATION_NODE,
  snapshot: { rawSha256: process.env.CLOUD_AGENTS_COMPOSE_RAW_SHA256, contentDigest: process.env.CLOUD_AGENTS_COMPOSE_CONTENT_DIGEST, restoredWorkspaceSha256: process.env.CLOUD_AGENTS_COMPOSE_RESTORED_SHA256, restoredArchiveSha256: process.env.CLOUD_AGENTS_COMPOSE_RESTORED_ARCHIVE_SHA256, sizeBytes: Number(process.env.CLOUD_AGENTS_COMPOSE_SNAPSHOT_SIZE), pvc: process.env.CLOUD_AGENTS_COMPOSE_PVC, pv: process.env.CLOUD_AGENTS_COMPOSE_PV, pod: process.env.CLOUD_AGENTS_COMPOSE_POD, podNode: process.env.CLOUD_AGENTS_COMPOSE_POD_NODE },
  workspaceId: process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE,
  sandboxId: process.env.CLOUD_AGENTS_COMPOSE_SANDBOX,
  generation: Number(process.env.CLOUD_AGENTS_COMPOSE_GENERATION),
  attempt: execution.spec?.attemptNumber,
  recoveryState: execution.spec?.recoveryState,
  recoveryMode: execution.spec?.recoveryMode,
  recoveryTargetId: execution.spec?.recoveryTargetId,
  recoverySourceTargetId: execution.spec?.recoverySourceTargetId,
  rtoMilliseconds: Number(process.env.CLOUD_AGENTS_COMPOSE_CROSS_RTO_MS),
  rpoBytes: 0,
};
if (`sha256:${evidence.snapshot.restoredWorkspaceSha256}` !== evidence.snapshot.contentDigest) throw new Error("restored workspace content digest mismatch");
if (evidence.attempt !== 2 || evidence.recoveryState !== "recovered" || evidence.recoveryMode !== "cross-node-takeover") throw new Error("recovery evidence did not record attempt 2 cross-node takeover");
writeFileSync(path, JSON.stringify(evidence, null, 2) + "\n", { mode: 0o600 });
process.stdout.write(`MCP_SKILL_RUNTIME_V1_CROSS_NODE_SNAPSHOT_EVIDENCE=${path} raw_sha256=${evidence.snapshot.rawSha256} restored_sha256=${evidence.snapshot.restoredWorkspaceSha256} size_bytes=${evidence.snapshot.sizeBytes} pvc=${evidence.snapshot.pvc} pv=${evidence.snapshot.pv} pod=${evidence.snapshot.pod} generation=${evidence.generation} attempt=${evidence.attempt}\n`);
NODE
    fi
  fi
  if [ "$recovery_cross_node" -eq 1 ]; then
    cleanup_codex_recovery_snapshot
  fi
}

run_selected_real_provider_recoveries() {
  skip_provider=$1
  [ "$capability_bound_recovery" -eq 1 ] || return 0
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
    if capability_bound_recovery_completed "$real_provider_environment_slug" "$provider_kind"; then
      continue
    fi
    run_real_provider_recovery "$provider_kind" "$provider_slug" \
      "$real_provider_environment_slug" "$real_provider_environment_label" \
      "$real_provider_workspace_id" "$real_provider_sandbox_id" "$real_provider_sandbox_generation" \
      "$real_provider_environment_profile_id" "$real_provider_expected_target" 0
  done
}

capability_bound_recovery_completed() {
  case " $capability_bound_recoveries " in
    *" $1:$2 "*) return 0 ;;
    *) return 1 ;;
  esac
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
capability_provider_enabled() {
  case "$1" in
    codex | claudeAgent | pi | deepseek-harness) return 0 ;;
    *) return 1 ;;
  esac
}

run_pending_cross_node_recovery() {
  [ "${cross_node_recovery_pending:-0}" -eq 1 ] || return 0
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
  if [ "${recovery_capability_bound:-0}" -eq 1 ]; then
    capability_bound_recoveries="$capability_bound_recoveries $cross_node_environment:$cross_node_provider_to_run"
  fi
  cross_node_recovery_pending=0
}

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
  if [ "$cross_node_recovery" -eq 1 ] && [ -n "$real_provider_credentials_directory" ]; then
    cross_node_recovery_pending=1
  elif [ "$capability_bound_recovery" -eq 1 ] && [ "$capability_bound_recovery_environment" != kubernetes ] && [ -n "$real_provider_credentials_directory" ] &&
    ! capability_bound_recovery_completed docker "$cross_node_provider_to_run"; then
    run_real_provider_recovery "$cross_node_provider_to_run" "$cross_node_provider_slug" docker Docker \
      "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" "$foundation_agent_generation" \
      "$foundation_agent_environment_profile_id" docker-compose-target 0
  fi
elif [ "$cross_node_recovery" -eq 1 ]; then
  echo "CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER must be selected by CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS" >&2
  exit 2
fi
if [ -n "$real_provider_credentials_directory" ]; then
  if [ "${CLOUD_AGENTS_COMPOSE_RECOVERY_ONLY:-0}" != 1 ]; then
    real_provider_run_prefix=compose-real
    real_provider_environment_slug=docker
    real_provider_environment_label=Docker
    real_provider_workspace_id=$foundation_agent_workspace_id
    real_provider_sandbox_id=$foundation_agent_sandbox_id
    real_provider_sandbox_generation=$foundation_agent_generation
    real_provider_environment_profile_id=$foundation_agent_environment_profile_id
    start_capability_mcp_fixture "$foundation_agent_runtime_id"
    for recovery_provider in $real_provider_kinds; do
      if real_provider_selected "$recovery_provider" && capability_provider_enabled "$recovery_provider"; then
        if ! run_capability_process_recovery "$recovery_provider"; then
          echo "capability process recovery failed provider=$recovery_provider" >&2
          exit 1
        fi
      fi
    done
    if [ "$cross_node_environment" = docker ]; then
      run_pending_cross_node_recovery
      real_provider_workspace_id=$foundation_agent_workspace_id
      real_provider_sandbox_id=$foundation_agent_sandbox_id
      real_provider_sandbox_generation=$foundation_agent_generation
      real_provider_environment_profile_id=$foundation_agent_environment_profile_id
      real_provider_expected_target=docker-compose-target-restore
    fi
    start_capability_mcp_fixture "$foundation_agent_runtime_id"
    run_selected_real_providers
    if [ "$remote_runtime" -eq 1 ]; then
      real_provider_run_prefix=compose-remote-real
      real_provider_environment_slug=remote-worker
      real_provider_environment_label=RemoteWorker
      real_provider_workspace_id=$remote_agent_workspace_id
      real_provider_sandbox_id=$remote_agent_sandbox_id
      real_provider_sandbox_generation=$remote_agent_generation
      real_provider_environment_profile_id=$remote_agent_environment_profile_id
      start_capability_mcp_fixture "$remote_agent_runtime_id"
      for recovery_provider in $real_provider_kinds; do
        if real_provider_selected "$recovery_provider" && capability_provider_enabled "$recovery_provider"; then
          run_capability_process_recovery "$recovery_provider" || exit 1
        fi
      done
      if [ "$cross_node_environment" = remote-worker ]; then
        run_pending_cross_node_recovery
        real_provider_workspace_id=$remote_agent_workspace_id
        real_provider_sandbox_id=$remote_agent_sandbox_id
        real_provider_sandbox_generation=$remote_agent_generation
        real_provider_environment_profile_id=$remote_agent_environment_profile_id
        real_provider_expected_target=$remote_target_restore_id
      fi
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
      start_capability_mcp_fixture "$kubernetes_agent_runtime_id"
      for recovery_provider in $real_provider_kinds; do
        if real_provider_selected "$recovery_provider" && capability_provider_enabled "$recovery_provider"; then
          run_capability_process_recovery "$recovery_provider" || exit 1
        fi
      done
      if [ "$cross_node_environment" = kubernetes ]; then
        run_pending_cross_node_recovery
        real_provider_workspace_id=$kubernetes_agent_workspace_id
        real_provider_sandbox_id=$kubernetes_agent_sandbox_id
        real_provider_sandbox_generation=$kubernetes_agent_generation
        real_provider_environment_profile_id=$kubernetes_agent_environment_profile_id
        real_provider_expected_target=$kubernetes_destination_target_id
      fi
      run_selected_real_providers
    fi
    if [ "$capability_transport_recovery" -eq 1 ] && [ "$capability_transport_recovery_ran" -ne 1 ]; then
      echo "capability transport recovery selector did not match an enabled real Provider environment" >&2
      exit 2
    fi
    if [ "$capability_process_recovery" -eq 1 ] && [ "$capability_process_recovery_ran" -ne 1 ]; then
      echo "capability process recovery selector did not match an enabled real Provider environment" >&2
      exit 2
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
      CLOUD_AGENTS_E2E_ENVIRONMENT=remote-worker \
      CLOUD_AGENTS_E2E_REMOTE_WORKER=1 \
      CLOUD_AGENTS_E2E_ADMIN_TOKEN_FILE="$smoke_directory/admin-token" \
      CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID="$remote_agent_runtime_id" \
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
      CLOUD_AGENTS_E2E_ENVIRONMENT=kubernetes \
      CLOUD_AGENTS_E2E_RECOVERY_FAULT=agent \
      CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID="$kubernetes_agent_runtime_id" \
      CLOUD_AGENTS_E2E_AGENT_TARGET_ID="$kubernetes_runtime_target_id" \
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

run_pending_cross_node_recovery

if [ "$kubernetes_runtime" -eq 1 ]; then
  kubernetes_agent_stale_generation=$kubernetes_agent_generation
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
  kubernetes_agent_current_generation=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/kubernetes-agent-sandbox-stopped.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(String(value.spec?.generation??""))')
  if [ "$capability_negative_test" -eq 1 ]; then
    for stale_provider in $real_provider_kinds; do
      assert_stale_capability_session kubernetes "$kubernetes_agent_workspace_id" "$kubernetes_agent_sandbox_id" \
        "$kubernetes_agent_stale_generation" "$kubernetes_agent_current_generation" "$kubernetes_agent_environment_profile_id" "$stale_provider"
    done
  fi
  kubernetes_agent_pvc_count=$(kubernetes_ctl -n "$kubernetes_active_namespace" get pvc \
    -l cloud-agents.dev/resource=foundation-workspace -o json | \
    CLOUD_AGENTS_COMPOSE_WORKSPACE="$kubernetes_agent_workspace_id" node -e \
      'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(String(value.items.filter((item)=>item.metadata?.annotations?.["cloud-agents.dev/workspace"]===process.env.CLOUD_AGENTS_COMPOSE_WORKSPACE).length))')
  test "$kubernetes_agent_pvc_count" -eq 1
fi

if [ "$remote_runtime" -eq 1 ]; then
  remote_agent_stale_generation=$remote_agent_generation
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
      'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.exit(value.spec?.observedState==="stopped"&&value.spec?.writerReleased===true&&value.spec?.cleanupPhase==="complete"?0:1)'; then
      break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 180 ]; then
      echo "Compose outbound RemoteWorker Agent Sandbox did not stop" >&2
      exit 1
    fi
    sleep 1
  done
  remote_agent_current_generation=$(CLOUD_AGENTS_COMPOSE_SANDBOX_FILE="$smoke_directory/remote-agent-sandbox-stopped.json" node -e \
    'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SANDBOX_FILE,"utf8"));process.stdout.write(String(value.spec?.generation??""))')
  if [ "$capability_negative_test" -eq 1 ]; then
    for stale_provider in $real_provider_kinds; do
      assert_stale_capability_session remote-worker "$remote_agent_workspace_id" "$remote_agent_sandbox_id" \
        "$remote_agent_stale_generation" "$remote_agent_current_generation" "$remote_agent_environment_profile_id" "$stale_provider"
    done
  fi
  if [ "$remote_agent_target" = "$remote_target_restore_id" ]; then
    remote_agent_volume=$(docker -H "tcp://127.0.0.1:$destination_docker_port" volume ls -q \
      --filter label=cloud-agents.dev/resource=foundation-workspace \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id" \
      --filter label=cloud-agents.dev/target="$remote_agent_target" \
      --filter label=cloud-agents.dev/workspace="$remote_agent_workspace_id")
  else
    remote_agent_volume=$(docker volume ls -q \
      --filter label=cloud-agents.dev/resource=foundation-workspace \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id" \
      --filter label=cloud-agents.dev/target="$remote_agent_target" \
      --filter label=cloud-agents.dev/workspace="$remote_agent_workspace_id")
  fi
  case "$remote_agent_volume" in
    '' | *' '*) echo "Compose outbound RemoteWorker Workspace volume inventory changed" >&2; exit 1 ;;
  esac
  if [ "$remote_agent_target" = "$remote_target_restore_id" ]; then
    docker -H "tcp://127.0.0.1:$destination_docker_port" volume rm "$remote_agent_volume" >/dev/null
  else
    docker volume rm "$remote_agent_volume" >/dev/null
  fi
fi

foundation_agent_stale_generation=$foundation_agent_generation
stop_foundation_agent
if [ "$capability_negative_test" -eq 1 ]; then
  for stale_provider in $real_provider_kinds; do
    assert_stale_capability_session docker "$foundation_agent_workspace_id" "$foundation_agent_sandbox_id" \
      "$foundation_agent_stale_generation" "$foundation_agent_generation" "$foundation_agent_environment_profile_id" "$stale_provider"
  done
fi
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
