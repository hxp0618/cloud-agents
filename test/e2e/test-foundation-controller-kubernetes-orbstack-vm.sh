#!/bin/sh
set -eu

usage() {
  echo "usage: $0 --vm VM --kubeconfig FILE --context CONTEXT --output-dir NEW_DIR [--fault-soak-only]" >&2
  exit 2
}

vm=
kubeconfig=
context=
output_dir=
fault_soak_only=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --vm | --kubeconfig | --context | --output-dir)
      [ "$#" -ge 2 ] || usage
      case "$1" in
        --vm) vm=$2 ;;
        --kubeconfig) kubeconfig=$2 ;;
        --context) context=$2 ;;
        --output-dir) output_dir=$2 ;;
      esac
      shift 2
      ;;
    --fault-soak-only)
      fault_soak_only=1
      shift
      ;;
    *) usage ;;
  esac
done

[ -n "$vm" ] && [ -n "$kubeconfig" ] && [ -n "$context" ] && [ -n "$output_dir" ] || usage
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)

for command_name in orb kubectl docker mise python3; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "required command is unavailable: $command_name" >&2
    exit 1
  }
done

[ -f "$kubeconfig" ] && [ ! -L "$kubeconfig" ] || {
  echo "kubeconfig must be a regular non-symlink file" >&2
  exit 1
}
kubeconfig=$(CDPATH= cd -- "$(dirname -- "$kubeconfig")" && printf '%s/%s\n' "$(pwd -P)" "$(basename -- "$kubeconfig")")
if kubeconfig_mode=$(stat -f '%Lp' "$kubeconfig" 2>/dev/null); then
  :
else
  kubeconfig_mode=$(stat -c '%a' "$kubeconfig")
fi
[ "$kubeconfig_mode" = 600 ] || {
  echo "kubeconfig mode must be 0600" >&2
  exit 1
}
[ ! -e "$output_dir" ] && [ ! -L "$output_dir" ] || {
  echo "output directory must not already exist" >&2
  exit 1
}

orb list | awk -v vm="$vm" '$1 == vm && $2 == "running" { found = 1 } END { exit found ? 0 : 1 }' || {
  echo "requested OrbStack VM is not running" >&2
  exit 1
}
kubectl --kubeconfig "$kubeconfig" --context "$context" config get-contexts -o name |
  grep -Fqx "$context" || {
  echo "explicit Kubernetes context was not found" >&2
  exit 1
}
kubectl --kubeconfig "$kubeconfig" --context "$context" get --raw=/readyz >/dev/null

vm_next_hop=$(orb -m "$vm" sh -lc 'hostname -I' | awk '{ print $1; exit }')
if ! node_values=$(kubectl --kubeconfig "$kubeconfig" --context "$context" get node -o json |
  python3 -c '
import ipaddress
import json
import sys

items = json.load(sys.stdin).get("items", [])
if len(items) != 1:
    raise SystemExit(1)
node = items[0]
pod_cidr = node.get("spec", {}).get("podCIDR", "")
internal = [
    value.get("address", "")
    for value in node.get("status", {}).get("addresses", [])
    if value.get("type") == "InternalIP"
]
internal_v4 = [value for value in internal if ipaddress.ip_address(value).version == 4]
if not pod_cidr or len(internal_v4) != 1:
    raise SystemExit(1)
print(pod_cidr)
print(internal_v4[0])
' 2>/dev/null); then
  echo "explicit context must contain exactly one node with one IPv4 InternalIP and a Pod CIDR" >&2
  exit 1
fi
pod_cidr=$(printf '%s\n' "$node_values" | sed -n '1p')
node_internal_ip=$(printf '%s\n' "$node_values" | sed -n '2p')
service_ip=$(kubectl --kubeconfig "$kubeconfig" --context "$context" get service kubernetes -n default -o jsonpath='{.spec.clusterIP}')
if ! service_cidr=$(kubectl --kubeconfig "$kubeconfig" --context "$context" \
  get servicecidrs.networking.k8s.io -o json |
  python3 -c '
import ipaddress
import json
import sys

items = json.load(sys.stdin).get("items", [])
cidrs = [cidr for item in items for cidr in item.get("spec", {}).get("cidrs", [])]
ipv4 = [cidr for cidr in cidrs if ipaddress.ip_network(cidr, strict=True).version == 4]
if len(items) != 1 or len(cidrs) != 1 or len(ipv4) != 1:
    raise SystemExit(1)
print(ipv4[0])
' 2>/dev/null); then
  echo "explicit context must expose one authoritative IPv4 ServiceCIDR" >&2
  exit 1
fi

helper_image='busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662'
docker --context orbstack image inspect "$helper_image" >/dev/null

network_values=$(python3 - "$pod_cidr" "$service_cidr" "$service_ip" "$vm_next_hop" "$node_internal_ip" <<'PY'
import ipaddress
import sys

try:
    pod = ipaddress.ip_network(sys.argv[1], strict=True)
    service = ipaddress.ip_network(sys.argv[2], strict=True)
    service_ip = ipaddress.ip_address(sys.argv[3])
    next_hop = ipaddress.ip_address(sys.argv[4])
    node_internal_ip = ipaddress.ip_address(sys.argv[5])
except ValueError:
    raise SystemExit("discovered route inputs are not canonical IP values")
if pod.version != 4 or service.version != 4 or service_ip.version != 4 or next_hop.version != 4:
    raise SystemExit("only IPv4 OrbStack/k3s routes are supported")
if next_hop != node_internal_ip:
    raise SystemExit("selected OrbStack VM address does not match the explicit context node InternalIP")
if service_ip not in service:
    raise SystemExit("Kubernetes service IP is outside the discovered service CIDR")
if pod.overlaps(service):
    raise SystemExit("Pod and service CIDRs overlap")
pod_probe = next(pod.hosts(), None)
if pod_probe is None:
    raise SystemExit("Pod CIDR has no usable host address")
print(pod_probe)
PY
)
pod_probe=$network_values

mkdir -m 700 "$output_dir"
output_dir=$(cd "$output_dir" && pwd -P)
foundation_output="$output_dir/foundation"
route_helper_name="cloud-agents-k8s-route-$$"
helper_started=0
helper_finished=0

exact_route_count() {
  route=$1
  docker --context orbstack run --rm --privileged --network host "$helper_image" \
    sh -c 'ip route show exact "$1" | wc -l' sh "$route" | tr -d '[:space:]'
}

pod_route_count=$(exact_route_count "$pod_cidr")
service_route_count=$(exact_route_count "$service_cidr")
python3 - "$output_dir/route-before.json" "$pod_route_count" "$service_route_count" <<'PY'
import json
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_text(json.dumps({
    "podExactRouteCount": int(sys.argv[2]),
    "serviceExactRouteCount": int(sys.argv[3]),
}, separators=(",", ":")) + "\n")
PY
if [ "$pod_route_count" -ne 0 ] || [ "$service_route_count" -ne 0 ]; then
  echo "target Docker network namespace already has an exact route for a requested CIDR" >&2
  exit 1
fi

finish_helper() {
  [ "$helper_started" -eq 1 ] || return 0
  [ "$helper_finished" -eq 0 ] || return 0
  helper_finished=1
  : > "$output_dir/route-stop"
  set +e
  helper_exit=$(docker --context orbstack wait "$route_helper_name" 2>/dev/null)
  wait_status=$?
  docker --context orbstack logs "$route_helper_name" >"$output_dir/route-helper.log" 2>&1
  logs_status=$?
  docker --context orbstack rm "$route_helper_name" >/dev/null 2>&1
  rm_status=$?
  set -e
  if [ "$wait_status" -ne 0 ] || [ -z "$helper_exit" ]; then
    helper_exit=1
  fi
  printf '%s\n' "$helper_exit" >"$output_dir/route-helper.exit"
  [ "$helper_exit" = 0 ] && [ "$logs_status" -eq 0 ] && [ "$rm_status" -eq 0 ]
}

on_exit() {
  original_status=$?
  trap - EXIT HUP INT TERM
  cleanup_status=0
  finish_helper || cleanup_status=1
  if [ "$original_status" -ne 0 ]; then
    exit "$original_status"
  fi
  exit "$cleanup_status"
}
trap on_exit EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

helper_started=1
helper_id=$(docker --context orbstack run -d \
  --name "$route_helper_name" \
  --label cloud-agents.dev/e2e-route-helper=true \
  --privileged \
  --network host \
  -v "$output_dir:/evidence" \
  -e POD_CIDR="$pod_cidr" \
  -e SERVICE_CIDR="$service_cidr" \
  -e SERVICE_IP="$service_ip" \
  -e POD_PROBE="$pod_probe" \
  -e VM_NEXT_HOP="$vm_next_hop" \
  "$helper_image" sh -eu -c '
pod_added=0
service_added=0
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  cleanup_status=0
  if [ "$service_added" -eq 1 ]; then
    ip route del "$SERVICE_CIDR" via "$VM_NEXT_HOP" || cleanup_status=1
  fi
  if [ "$pod_added" -eq 1 ]; then
    ip route del "$POD_CIDR" via "$VM_NEXT_HOP" || cleanup_status=1
  fi
  pod_after=$(ip route show exact "$POD_CIDR" | wc -l | tr -d "[:space:]")
  service_after=$(ip route show exact "$SERVICE_CIDR" | wc -l | tr -d "[:space:]")
  printf "{\"podExactRouteCount\":%s,\"serviceExactRouteCount\":%s}\n" \
    "$pod_after" "$service_after" > /evidence/route-after.json
  if [ "$pod_after" -ne 0 ] || [ "$service_after" -ne 0 ]; then cleanup_status=1; fi
  if [ "$status" -ne 0 ]; then exit "$status"; fi
  exit "$cleanup_status"
}
trap cleanup EXIT
trap "exit 129" HUP
trap "exit 130" INT
trap "exit 143" TERM

pod_before=$(ip route show exact "$POD_CIDR" | wc -l | tr -d "[:space:]")
service_before=$(ip route show exact "$SERVICE_CIDR" | wc -l | tr -d "[:space:]")
[ "$pod_before" -eq 0 ] && [ "$service_before" -eq 0 ]
ip route add "$POD_CIDR" via "$VM_NEXT_HOP"
pod_added=1
ip route add "$SERVICE_CIDR" via "$VM_NEXT_HOP"
service_added=1
ip route get "$POD_PROBE" | grep -Fq "via $VM_NEXT_HOP"
ip route get "$SERVICE_IP" | grep -Fq "via $VM_NEXT_HOP"
ping -c 1 -W 3 "$POD_PROBE" >/dev/null
nc -z -w 3 "$SERVICE_IP" 443
printf "%s\n" "{\"podRouteViaVm\":true,\"serviceRouteViaVm\":true,\"podGatewayPing\":true,\"serviceApiTcp\":true}" \
  > /evidence/route-repaired.json
: > /evidence/route-ready
while [ ! -e /evidence/route-stop ]; do sleep 1; done
')
[ -n "$helper_id" ]

ready=0
attempt=0
while [ "$attempt" -lt 60 ]; do
  if [ -e "$output_dir/route-ready" ]; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
[ "$ready" -eq 1 ] || {
  echo "route helper did not become ready" >&2
  exit 1
}

set +e
if [ "$fault_soak_only" -eq 1 ]; then
  (
    cd "$repo_root"
    KUBECONFIG="$kubeconfig" CLOUD_AGENTS_KUBERNETES_CONTEXT="$context" \
      mise exec -- zsh -lc 'exec node "$@"' cloud-agents-k8s \
        test/e2e/test-foundation-controller-kubernetes.mjs "$foundation_output" --fault-soak-only
  ) >"$output_dir/harness.log" 2>&1
else
  (
    cd "$repo_root"
    KUBECONFIG="$kubeconfig" CLOUD_AGENTS_KUBERNETES_CONTEXT="$context" \
      mise exec -- zsh -lc 'exec node "$@"' cloud-agents-k8s \
        test/e2e/test-foundation-controller-kubernetes.mjs "$foundation_output"
  ) >"$output_dir/harness.log" 2>&1
fi
harness_exit=$?
set -e
printf '%s\n' "$harness_exit" >"$output_dir/harness.exit"

helper_cleanup_status=0
finish_helper || helper_cleanup_status=1
route_restored=0
if python3 - "$output_dir/route-after.json" <<'PY'
import json
import pathlib
import sys

value = json.loads(pathlib.Path(sys.argv[1]).read_text())
if value != {"podExactRouteCount": 0, "serviceExactRouteCount": 0}:
    raise SystemExit(1)
PY
then
  route_restored=1
fi

namespace_count=$(kubectl --kubeconfig "$kubeconfig" --context "$context" get ns -o name |
  awk '/^namespace\/foundation-k8s-[a-z0-9]+(-ops)?$/ { count++ } END { print count + 0 }')
foundation_container_count=$(docker --context orbstack ps -a --format '{{.Names}}' |
  awk '/^foundation-k8s-/ { count++ } END { print count + 0 }')
route_helper_count=$(docker --context orbstack ps -a --filter "name=^/${route_helper_name}$" -q | wc -l | tr -d '[:space:]')
python3 - "$output_dir/cleanup.json" "$namespace_count" "$foundation_container_count" "$route_helper_count" <<'PY'
import json
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_text(json.dumps({
    "foundationNamespaceCount": int(sys.argv[2]),
    "foundationContainerCount": int(sys.argv[3]),
    "routeHelperContainerCount": int(sys.argv[4]),
}, separators=(",", ":")) + "\n")
PY

if [ "$helper_cleanup_status" -ne 0 ] || [ "$route_restored" -ne 1 ] || \
  [ "$namespace_count" -ne 0 ] || [ "$foundation_container_count" -ne 0 ] || \
  [ "$route_helper_count" -ne 0 ]; then
  exit 1
fi
exit "$harness_exit"
