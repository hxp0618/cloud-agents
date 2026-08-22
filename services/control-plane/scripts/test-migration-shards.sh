#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"
script_path="$script_dir/$(basename -- "${BASH_SOURCE[0]}")"
validator_dir="$script_dir/migration-shard-validator"
validator_source="$validator_dir/main.go"

usage() {
  cat <<'EOF'
Usage:
  test-migration-shards.sh plan [--shards N]
  test-migration-shards.sh run --output-dir ABSOLUTE_PATH [options]

Options:
  --shards N          Number of mutually exclusive shards (default: 8, maximum: 64)
  --jobs N            Concurrent shard processes for run mode (default: 2)
  --test-parallel N   go test -parallel value per shard (default: 2)
  --timeout DURATION  Per-shard Go test timeout (default: 20m)
  --race              Run every shard with the race detector
  --output-dir PATH   New absolute evidence directory; run mode only
  --help              Show this help

Set CLOUD_AGENTS_GO to an exact Go binary path when the ambient go command is
not the repository-declared Go 1.26.6 toolchain.
EOF
}

fail() {
  echo "$*" >&2
  exit 1
}

require_positive_integer() {
  local name=$1
  local value=$2
  if [[ ! $value =~ ^[1-9][0-9]*$ ]]; then
    fail "$name must be a positive integer"
  fi
}

sha256_file() {
  local file=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  fail "sha256sum or shasum is required"
}

mode=${1:-}
case "$mode" in
  plan | run)
    shift
    ;;
  --help | -h)
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

shard_count=8
job_count=2
jobs_explicit=0
test_parallel=2
test_timeout=20m
race_enabled=0
output_dir=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --shards)
      [[ $# -ge 2 ]] || fail "--shards requires a value"
      shard_count=$2
      shift 2
      ;;
    --jobs)
      [[ $# -ge 2 ]] || fail "--jobs requires a value"
      job_count=$2
      jobs_explicit=1
      shift 2
      ;;
    --test-parallel)
      [[ $# -ge 2 ]] || fail "--test-parallel requires a value"
      test_parallel=$2
      shift 2
      ;;
    --timeout)
      [[ $# -ge 2 ]] || fail "--timeout requires a value"
      test_timeout=$2
      shift 2
      ;;
    --race)
      race_enabled=1
      shift
      ;;
    --output-dir)
      [[ $# -ge 2 ]] || fail "--output-dir requires a value"
      output_dir=$2
      shift 2
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

require_positive_integer "--shards" "$shard_count"
require_positive_integer "--jobs" "$job_count"
require_positive_integer "--test-parallel" "$test_parallel"
if ((shard_count > 64)); then
  fail "--shards must not exceed 64"
fi
if ((job_count > shard_count)); then
  if ((jobs_explicit == 1)); then
    fail "--jobs must not exceed --shards"
  fi
  job_count=$shard_count
fi
if [[ ! $test_timeout =~ ^[1-9][0-9]*(ms|s|m|h)$ ]]; then
  fail "--timeout must be a positive Go duration using ms, s, m, or h"
fi
if [[ $mode == plan && -n $output_dir ]]; then
  fail "--output-dir is valid only in run mode"
fi
if [[ $mode == run && -z $output_dir ]]; then
  fail "run mode requires --output-dir"
fi

go_command=${CLOUD_AGENTS_GO:-go}
if ! command -v "$go_command" >/dev/null 2>&1; then
  fail "Go command not found: $go_command"
fi
go_version=$("$go_command" version | awk '{print $3}')
if [[ $go_version != go1.26.6 ]]; then
  fail "Go 1.26.6 is required; observed $go_version"
fi
[[ -f $validator_source ]] || fail "migration shard JSON validator source is missing"
module_path=$(env GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  "$go_command" -C "$module_dir" list -m -f '{{.Path}}')
migration_package="$module_path/internal/migration"

external_gate_variables=(
  CLOUD_AGENTS_REQUIRE_CATALOG_PARSE_TEST
  CLOUD_AGENTS_CATALOG_PARSE_DSN
  CLOUD_AGENTS_REQUIRE_POSTGRES_PROJECTION_TEST
  CLOUD_AGENTS_PROJECTION_ADMIN_URL
  CLOUD_AGENTS_PROJECTION_MIGRATION_URL
  CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR
  CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM
  CLOUD_AGENTS_PROJECTION_INSTANCE
  CLOUD_AGENTS_PROJECTION_IMAGE_ID
  CLOUD_AGENTS_PROJECTION_CONTAINER_ARCH
  CLOUD_AGENTS_PROJECTION_PROFILE
  CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK
  CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK_REVOKED
  CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT
)
for external_gate_variable in "${external_gate_variables[@]}"; do
  if [[ -n $(printenv "$external_gate_variable" 2>/dev/null || true) ]]; then
    fail "$external_gate_variable must be unset for the default migration shard suite"
  fi
done

temporary_dir=""
active_pids=()
active_pgids=()
active_shards=()
work_dir=""
validator_binary=""
run_in_progress=0
launch_in_progress=0
pending_signal_exit_code=""
pending_signal_name=""
signal_deferred_during_launch=0

cleanup_plan() {
  if [[ -n $temporary_dir && -d $temporary_dir ]]; then
    find "$temporary_dir" -depth -delete
  fi
}

cleanup_runtime_tools() {
  if [[ -n $validator_binary && -f $validator_binary ]]; then
    rm -f -- "$validator_binary"
  fi
}

cleanup_all() {
  cleanup_runtime_tools
  cleanup_plan
}

process_group_alive() {
  local pgid=$1
  kill -0 -- "-$pgid" 2>/dev/null
}

terminate_active_process_groups() {
  local index
  local attempt
  local remaining
  local cleanup_failed=0

  index=0
  while ((index < ${#active_pgids[@]})); do
    if process_group_alive "${active_pgids[$index]}"; then
      kill -TERM -- "-${active_pgids[$index]}" 2>/dev/null || true
    fi
    index=$((index + 1))
  done

  attempt=0
  while ((attempt < 50)); do
    remaining=0
    index=0
    while ((index < ${#active_pgids[@]})); do
      if process_group_alive "${active_pgids[$index]}"; then
        remaining=1
      fi
      index=$((index + 1))
    done
    ((remaining == 0)) && break
    sleep 0.1
    attempt=$((attempt + 1))
  done

  index=0
  while ((index < ${#active_pgids[@]})); do
    if process_group_alive "${active_pgids[$index]}"; then
      kill -KILL -- "-${active_pgids[$index]}" 2>/dev/null || true
    fi
    index=$((index + 1))
  done

  index=0
  while ((index < ${#active_pids[@]})); do
    wait "${active_pids[$index]}" 2>/dev/null || true
    index=$((index + 1))
  done

  attempt=0
  while ((attempt < 20)); do
    remaining=0
    index=0
    while ((index < ${#active_pgids[@]})); do
      if process_group_alive "${active_pgids[$index]}"; then
        remaining=1
      fi
      index=$((index + 1))
    done
    ((remaining == 0)) && break
    sleep 0.1
    attempt=$((attempt + 1))
  done

  index=0
  while ((index < ${#active_pgids[@]})); do
    if process_group_alive "${active_pgids[$index]}"; then
      cleanup_failed=1
    fi
    index=$((index + 1))
  done
  active_pids=()
  active_pgids=()
  active_shards=()
  return "$cleanup_failed"
}

write_run_status() {
  local status=$1
  local temporary_status="$work_dir/.run-status.$$"
  printf '%s\n' "$status" >"$temporary_status"
  mv -- "$temporary_status" "$work_dir/run-status.txt"
}

fail_run() {
  write_run_status FAIL
  run_in_progress=0
  fail "$*"
}

abort_run_for_signal() {
  local exit_code=$1
  local signal_name=$2
  local cleanup_status=complete
  trap - EXIT INT TERM
  set +e
  if ! terminate_active_process_groups; then
    cleanup_status=failed
  fi
  cleanup_runtime_tools
  if ((run_in_progress == 1)) && [[ -n $work_dir && -d $work_dir ]]; then
    {
      printf 'key\tvalue\n'
      printf 'status\tABORTED\n'
      printf 'signal\t%s\n' "$signal_name"
      printf 'exit_code\t%s\n' "$exit_code"
      printf 'deferred_during_launch\t%s\n' "$signal_deferred_during_launch"
      printf 'process_group_cleanup\t%s\n' "$cleanup_status"
      printf 'recorded_at_utc\t%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    } >"$work_dir/run-aborted.tsv"
    write_run_status ABORTED
  fi
  cleanup_plan
  exit "$exit_code"
}

on_signal() {
  local exit_code=$1
  local signal_name=$2

  if ((launch_in_progress == 1)); then
    if [[ -z $pending_signal_name ]]; then
      pending_signal_exit_code=$exit_code
      pending_signal_name=$signal_name
      signal_deferred_during_launch=1
    fi
    return 0
  fi
  if [[ -n $pending_signal_name ]]; then
    abort_run_for_signal "$pending_signal_exit_code" "$pending_signal_name"
  fi
  abort_run_for_signal "$exit_code" "$signal_name"
}

consume_pending_signal() {
  if [[ -n $pending_signal_name ]]; then
    abort_run_for_signal "$pending_signal_exit_code" "$pending_signal_name"
  fi
}

trap cleanup_all EXIT
trap 'on_signal 130 INT' INT
trap 'on_signal 143 TERM' TERM

if [[ $mode == plan ]]; then
  temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/cag-migration-shards.XXXXXX")
  work_dir=$temporary_dir
else
  case "$output_dir" in
    /*) ;;
    *) fail "--output-dir must be absolute" ;;
  esac
  [[ $output_dir != / ]] || fail "--output-dir must not be the filesystem root"
  output_base=${output_dir##*/}
  [[ -n $output_base && $output_base != . && $output_base != .. ]] || fail "--output-dir has an invalid basename"
  output_parent=${output_dir%/*}
  if [[ -z $output_parent ]]; then
    output_parent=/
  fi
  [[ -d $output_parent ]] || fail "output parent does not exist: $output_parent"
  [[ -w $output_parent ]] || fail "output parent is not writable: $output_parent"
  output_parent=$(cd -- "$output_parent" && pwd -P)
  output_dir="$output_parent/$output_base"
  case "$output_dir" in
    "$repo_root" | "$repo_root"/*) fail "--output-dir must be outside the repository" ;;
  esac
  [[ ! -e $output_dir ]] || fail "--output-dir already exists: $output_dir"
  if [[ -n $(git -C "$repo_root" status --porcelain=v1 --untracked-files=all) ]]; then
    fail "run mode requires a clean repository worktree"
  fi
  mkdir -- "$output_dir"
  work_dir=$output_dir
fi

raw_list="$work_dir/go-test-list.raw"
all_tests="$work_dir/all-tests.txt"
invalid_tests="$work_dir/invalid-test-names.txt"
source_commit=$(git -C "$repo_root" rev-parse 'HEAD^{commit}')
source_tree=$(git -C "$repo_root" rev-parse 'HEAD^{tree}')
control_plane_tree=$(git -C "$repo_root" rev-parse HEAD:services/control-plane)
go_mod_sha256=$(sha256_file "$module_dir/go.mod")
go_sum_sha256=$(sha256_file "$module_dir/go.sum")
script_sha256=$(sha256_file "$script_path")
validator_source_sha256=$(sha256_file "$validator_source")

if ! env GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  "$go_command" -C "$module_dir" test -list '^Test' ./internal/migration >"$raw_list"; then
  fail "go test -list failed"
fi

LC_ALL=C awk '/^Test[A-Za-z0-9_]+$/' "$raw_list" | LC_ALL=C sort >"$all_tests"
LC_ALL=C awk '/^Test/ && $0 !~ /^Test[A-Za-z0-9_]+$/' "$raw_list" >"$invalid_tests"
if [[ -s $invalid_tests ]]; then
  fail "top-level test names outside the closed ASCII identifier grammar were observed"
fi
test_count=$(wc -l <"$all_tests" | tr -d ' ')
require_positive_integer "top-level test count" "$test_count"
if ((shard_count > test_count)); then
  fail "--shards exceeds the top-level test count"
fi
if [[ -n $(LC_ALL=C uniq -d "$all_tests") ]]; then
  fail "duplicate top-level test names were observed"
fi

shard_index=0
while ((shard_index < shard_count)); do
  printf -v shard_name 'shard-%02d' "$shard_index"
  mkdir -- "$work_dir/$shard_name"
  : >"$work_dir/$shard_name/tests.txt"
  shard_index=$((shard_index + 1))
done

test_index=0
while IFS= read -r test_name; do
  shard_index=$((test_index % shard_count))
  printf -v shard_name 'shard-%02d' "$shard_index"
  printf '%s\n' "$test_name" >>"$work_dir/$shard_name/tests.txt"
  test_index=$((test_index + 1))
done <"$all_tests"

union_tests="$work_dir/shard-union.txt"
duplicate_tests="$work_dir/shard-duplicates.txt"
cat "$work_dir"/shard-*/tests.txt | LC_ALL=C sort >"$union_tests"
cat "$work_dir"/shard-*/tests.txt | LC_ALL=C sort | LC_ALL=C uniq -d >"$duplicate_tests"
if [[ -s $duplicate_tests ]] || ! cmp -s "$all_tests" "$union_tests"; then
  fail "shard union is not a mutually exclusive exhaustive copy of the test list"
fi

shards_tsv="$work_dir/shards.tsv"
printf 'shard\ttests_sha256\tregex_sha256\ttest_count\n' >"$shards_tsv"
shard_index=0
while ((shard_index < shard_count)); do
  printf -v shard_name 'shard-%02d' "$shard_index"
  shard_dir="$work_dir/$shard_name"
  regex='^('
  separator=''
  while IFS= read -r test_name; do
    regex+="$separator$test_name"
    separator='|'
  done <"$shard_dir/tests.txt"
  regex+=')$'
  printf '%s\n' "$regex" >"$shard_dir/regex.txt"
  shard_test_count=$(wc -l <"$shard_dir/tests.txt" | tr -d ' ')
  printf '%s\t%s\t%s\t%s\n' \
    "$shard_name" \
    "$(sha256_file "$shard_dir/tests.txt")" \
    "$(sha256_file "$shard_dir/regex.txt")" \
    "$shard_test_count" >>"$shards_tsv"
  shard_index=$((shard_index + 1))
done

test_list_sha256=$(sha256_file "$all_tests")
go_environment=$(env GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  "$go_command" -C "$module_dir" env GOOS GOARCH CGO_ENABLED GOROOT)
goos=$(printf '%s\n' "$go_environment" | sed -n '1p')
goarch=$(printf '%s\n' "$go_environment" | sed -n '2p')
cgo_enabled=$(printf '%s\n' "$go_environment" | sed -n '3p')
goroot=$(printf '%s\n' "$go_environment" | sed -n '4p')

metadata_tsv="$work_dir/metadata.tsv"
{
  printf 'key\tvalue\n'
  printf 'source_commit\t%s\n' "$source_commit"
  printf 'source_tree\t%s\n' "$source_tree"
  printf 'control_plane_tree\t%s\n' "$control_plane_tree"
  printf 'script_sha256\t%s\n' "$script_sha256"
  printf 'validator_source_sha256\t%s\n' "$validator_source_sha256"
  printf 'migration_package\t%s\n' "$migration_package"
  printf 'go_mod_sha256\t%s\n' "$go_mod_sha256"
  printf 'go_sum_sha256\t%s\n' "$go_sum_sha256"
  printf 'go_version\t%s\n' "$go_version"
  printf 'goos\t%s\n' "$goos"
  printf 'goarch\t%s\n' "$goarch"
  printf 'cgo_enabled\t%s\n' "$cgo_enabled"
  printf 'goroot\t%s\n' "$goroot"
  printf 'test_list_sha256\t%s\n' "$test_list_sha256"
  printf 'test_count\t%s\n' "$test_count"
  printf 'shard_count\t%s\n' "$shard_count"
  printf 'job_count\t%s\n' "$job_count"
  printf 'test_parallel\t%s\n' "$test_parallel"
  printf 'test_timeout\t%s\n' "$test_timeout"
  printf 'race_enabled\t%s\n' "$race_enabled"
} >"$metadata_tsv"

if [[ $mode == plan ]]; then
  cat "$metadata_tsv"
  cat "$shards_tsv"
  echo "Migration shard plan: PASS tests=$test_count shards=$shard_count list_sha256=$test_list_sha256"
  exit 0
fi

validator_binary="$work_dir/.migration-shard-validator"
if ! env GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  "$go_command" -C "$module_dir" build -trimpath -buildvcs=false \
  -o "$validator_binary" ./scripts/migration-shard-validator; then
  fail "migration shard JSON validator build failed"
fi

run_shard() {
  local index=$1
  local start_gate=$2
  local name
  local dir
  local regex_value
  local started_epoch
  local finished_epoch
  local status
  local -a go_args

  printf -v name 'shard-%02d' "$index"
  dir="$work_dir/$name"
  while [[ ! -f $start_gate ]]; do
    sleep 0.05
  done
  regex_value=$(sed -n '1p' "$dir/regex.txt")
  started_epoch=$(date +%s)
  date -u '+%Y-%m-%dT%H:%M:%SZ' >"$dir/started-at.txt"
  go_args=(-C "$module_dir" test -json -count=1 -shuffle=off -parallel "$test_parallel" -run "$regex_value" -timeout "$test_timeout")
  if ((race_enabled == 1)); then
    go_args+=(-race)
  fi
  go_args+=(./internal/migration)

  if env GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    "$go_command" "${go_args[@]}" >"$dir/go-test.jsonl" 2>"$dir/go-test.stderr"; then
    status=0
  else
    status=$?
  fi
  finished_epoch=$(date +%s)
  date -u '+%Y-%m-%dT%H:%M:%SZ' >"$dir/finished-at.txt"
  printf '%s\n' "$status" >"$dir/exit-code.txt"
  printf '%s\n' "$((finished_epoch - started_epoch))" >"$dir/elapsed-seconds.txt"
  sha256_file "$dir/go-test.jsonl" >"$dir/go-test-jsonl.sha256"
  sha256_file "$dir/go-test.stderr" >"$dir/go-test-stderr.sha256"
  echo "MIGRATION_SHARD shard=$name status=$status elapsed_seconds=$((finished_epoch - started_epoch))"
  return "$status"
}

run_failed=0
run_in_progress=1
date -u '+%Y-%m-%dT%H:%M:%SZ' >"$work_dir/run-started-at.txt"
batch_start=0
while ((batch_start < shard_count)); do
  active_pids=()
  active_pgids=()
  active_shards=()
  batch_offset=0
  while ((batch_offset < job_count && batch_start + batch_offset < shard_count)); do
    shard_index=$((batch_start + batch_offset))
    printf -v shard_name 'shard-%02d' "$shard_index"
    shard_dir="$work_dir/$shard_name"
    start_gate="$shard_dir/start-authorized.txt"
    launch_in_progress=1
    set -m
    run_shard "$shard_index" "$start_gate" &
    shard_pid=$!
    set +m
    active_pids+=("$shard_pid")
    active_pgids+=("$shard_pid")
    active_shards+=("$shard_name")
    observed_pgid=$(ps -o pgid= -p "$shard_pid" 2>/dev/null | tr -d '[:space:]' || true)
    if [[ ! $observed_pgid =~ ^[1-9][0-9]*$ || $observed_pgid != "$shard_pid" ]]; then
      kill -TERM "$shard_pid" 2>/dev/null || true
      wait "$shard_pid" 2>/dev/null || true
      terminate_active_process_groups || true
      launch_in_progress=0
      consume_pending_signal
      fail_run "failed to establish an independent process group for $shard_name"
    fi
    {
      printf 'key\tvalue\n'
      printf 'wrapper_pid\t%s\n' "$shard_pid"
      printf 'process_group_id\t%s\n' "$observed_pgid"
    } >"$shard_dir/process-group.tsv"
    launch_in_progress=0
    consume_pending_signal
    : >"$start_gate"
    batch_offset=$((batch_offset + 1))
  done
  batch_offset=0
  batch_has_residue=0
  while ((batch_offset < ${#active_pids[@]})); do
    if ! wait "${active_pids[$batch_offset]}"; then
      run_failed=1
    fi
    if process_group_alive "${active_pgids[$batch_offset]}"; then
      echo "process group remained alive after ${active_shards[$batch_offset]} wrapper exit" >&2
      run_failed=1
      batch_has_residue=1
      break
    fi
    batch_offset=$((batch_offset + 1))
  done
  if ((batch_has_residue == 1)); then
    terminate_active_process_groups || true
  fi
  active_pids=()
  active_pgids=()
  active_shards=()
  batch_start=$((batch_start + job_count))
done
date -u '+%Y-%m-%dT%H:%M:%SZ' >"$work_dir/run-finished-at.txt"

final_source_commit=$(git -C "$repo_root" rev-parse 'HEAD^{commit}')
final_source_tree=$(git -C "$repo_root" rev-parse 'HEAD^{tree}')
final_control_plane_tree=$(git -C "$repo_root" rev-parse HEAD:services/control-plane)
if [[ $final_source_commit != "$source_commit" || $final_source_tree != "$source_tree" || $final_control_plane_tree != "$control_plane_tree" ]]; then
  fail_run "source identity changed during the shard run"
fi
if [[ -n $(git -C "$repo_root" status --porcelain=v1 --untracked-files=all) ]]; then
  fail_run "repository worktree changed during the shard run"
fi

validation_failed=0
validation_tsv="$work_dir/validation.tsv"
printf 'shard\tvalidator_exit_code\tvalidation_sha256\tplanned_count\trun_count\tpass_count\tskip_count\tfail_count\tpackage_pass_count\n' >"$validation_tsv"
shard_index=0
while ((shard_index < shard_count)); do
  printf -v shard_name 'shard-%02d' "$shard_index"
  shard_dir="$work_dir/$shard_name"
  validator_exit_code=0
  if "$validator_binary" \
    --tests "$shard_dir/tests.txt" \
    --json "$shard_dir/go-test.jsonl" \
    --package "$migration_package" \
    --output "$shard_dir/validation.tsv" \
    >"$shard_dir/validation.stdout" 2>"$shard_dir/validation.stderr"; then
    validator_exit_code=0
  else
    validator_exit_code=$?
    validation_failed=1
  fi
  printf '%s\n' "$validator_exit_code" >"$shard_dir/validation-exit-code.txt"
  sha256_file "$shard_dir/validation.stdout" >"$shard_dir/validation-stdout.sha256"
  sha256_file "$shard_dir/validation.stderr" >"$shard_dir/validation-stderr.sha256"
  validation_sha256=-
  planned_count=-
  run_count=-
  pass_count=-
  skip_count=-
  fail_count=-
  package_pass_count=-
  if ((validator_exit_code == 0)); then
    [[ -s $shard_dir/validation.tsv ]] || fail_run "validator did not publish a result for $shard_name"
    [[ ! -s $shard_dir/validation.stdout ]] || validation_failed=1
    [[ ! -s $shard_dir/validation.stderr ]] || validation_failed=1
    validation_sha256=$(sha256_file "$shard_dir/validation.tsv")
    printf '%s\n' "$validation_sha256" >"$shard_dir/validation.sha256"
    planned_count=$(awk -F '\t' '$1 == "planned_count" { print $2 }' "$shard_dir/validation.tsv")
    run_count=$(awk -F '\t' '$1 == "run_count" { print $2 }' "$shard_dir/validation.tsv")
    pass_count=$(awk -F '\t' '$1 == "pass_count" { print $2 }' "$shard_dir/validation.tsv")
    skip_count=$(awk -F '\t' '$1 == "skip_count" { print $2 }' "$shard_dir/validation.tsv")
    fail_count=$(awk -F '\t' '$1 == "fail_count" { print $2 }' "$shard_dir/validation.tsv")
    package_pass_count=$(awk -F '\t' '$1 == "package_pass_count" { print $2 }' "$shard_dir/validation.tsv")
  fi
  [[ ! -s $shard_dir/go-test.stderr ]] || validation_failed=1
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$shard_name" "$validator_exit_code" "$validation_sha256" "$planned_count" \
    "$run_count" "$pass_count" "$skip_count" "$fail_count" "$package_pass_count" >>"$validation_tsv"
  shard_index=$((shard_index + 1))
done
cleanup_runtime_tools
validation_tsv_sha256=$(sha256_file "$validation_tsv")
printf '%s\n' "$validation_tsv_sha256" >"$work_dir/validation.sha256"

results_tsv="$work_dir/results.tsv"
printf 'shard\texit_code\telapsed_seconds\tjsonl_sha256\tstderr_sha256\tvalidator_exit_code\tvalidation_sha256\n' >"$results_tsv"
shard_index=0
while ((shard_index < shard_count)); do
  printf -v shard_name 'shard-%02d' "$shard_index"
  shard_dir="$work_dir/$shard_name"
  [[ -s $shard_dir/exit-code.txt ]] || fail_run "missing result for $shard_name"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$shard_name" \
    "$(sed -n '1p' "$shard_dir/exit-code.txt")" \
    "$(sed -n '1p' "$shard_dir/elapsed-seconds.txt")" \
    "$(sed -n '1p' "$shard_dir/go-test-jsonl.sha256")" \
    "$(sed -n '1p' "$shard_dir/go-test-stderr.sha256")" \
    "$(sed -n '1p' "$shard_dir/validation-exit-code.txt")" \
    "$(if [[ -s $shard_dir/validation.sha256 ]]; then sed -n '1p' "$shard_dir/validation.sha256"; else printf '%s' -; fi)" >>"$results_tsv"
  shard_index=$((shard_index + 1))
done

if ((run_failed != 0 || validation_failed != 0)); then
  write_run_status FAIL
  run_in_progress=0
  echo "Migration shards: FAIL tests=$test_count shards=$shard_count list_sha256=$test_list_sha256 output=$work_dir" >&2
  exit 1
fi

write_run_status PASS
run_in_progress=0
trap - INT TERM
echo "Migration shards: PASS tests=$test_count shards=$shard_count list_sha256=$test_list_sha256 output=$work_dir"
