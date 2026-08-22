#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"
script_path="$script_dir/$(basename -- "${BASH_SOURCE[0]}")"

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

cleanup_plan() {
  if [[ -n $temporary_dir && -d $temporary_dir ]]; then
    find "$temporary_dir" -depth -delete
  fi
}

on_signal() {
  local exit_code=$1
  local pid
  trap - EXIT INT TERM
  for pid in "${active_pids[@]}"; do
    kill -TERM "$pid" 2>/dev/null || true
  done
  for pid in "${active_pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  cleanup_plan
  exit "$exit_code"
}

trap cleanup_plan EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

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

run_shard() {
  local index=$1
  local name
  local dir
  local regex_value
  local started_epoch
  local finished_epoch
  local status
  local -a go_args

  printf -v name 'shard-%02d' "$index"
  dir="$work_dir/$name"
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
date -u '+%Y-%m-%dT%H:%M:%SZ' >"$work_dir/run-started-at.txt"
batch_start=0
while ((batch_start < shard_count)); do
  active_pids=()
  batch_offset=0
  while ((batch_offset < job_count && batch_start + batch_offset < shard_count)); do
    shard_index=$((batch_start + batch_offset))
    run_shard "$shard_index" &
    active_pids+=("$!")
    batch_offset=$((batch_offset + 1))
  done
  batch_offset=0
  while ((batch_offset < ${#active_pids[@]})); do
    if ! wait "${active_pids[$batch_offset]}"; then
      run_failed=1
    fi
    batch_offset=$((batch_offset + 1))
  done
  active_pids=()
  batch_start=$((batch_start + job_count))
done
date -u '+%Y-%m-%dT%H:%M:%SZ' >"$work_dir/run-finished-at.txt"

final_source_commit=$(git -C "$repo_root" rev-parse 'HEAD^{commit}')
final_source_tree=$(git -C "$repo_root" rev-parse 'HEAD^{tree}')
final_control_plane_tree=$(git -C "$repo_root" rev-parse HEAD:services/control-plane)
if [[ $final_source_commit != "$source_commit" || $final_source_tree != "$source_tree" || $final_control_plane_tree != "$control_plane_tree" ]]; then
  fail "source identity changed during the shard run"
fi
if [[ -n $(git -C "$repo_root" status --porcelain=v1 --untracked-files=all) ]]; then
  fail "repository worktree changed during the shard run"
fi

results_tsv="$work_dir/results.tsv"
printf 'shard\texit_code\telapsed_seconds\tjsonl_sha256\tstderr_sha256\n' >"$results_tsv"
shard_index=0
while ((shard_index < shard_count)); do
  printf -v shard_name 'shard-%02d' "$shard_index"
  shard_dir="$work_dir/$shard_name"
  [[ -s $shard_dir/exit-code.txt ]] || fail "missing result for $shard_name"
  printf '%s\t%s\t%s\t%s\t%s\n' \
    "$shard_name" \
    "$(sed -n '1p' "$shard_dir/exit-code.txt")" \
    "$(sed -n '1p' "$shard_dir/elapsed-seconds.txt")" \
    "$(sed -n '1p' "$shard_dir/go-test-jsonl.sha256")" \
    "$(sed -n '1p' "$shard_dir/go-test-stderr.sha256")" >>"$results_tsv"
  shard_index=$((shard_index + 1))
done

if ((run_failed != 0)); then
  echo "Migration shards: FAIL tests=$test_count shards=$shard_count list_sha256=$test_list_sha256 output=$work_dir" >&2
  exit 1
fi

echo "Migration shards: PASS tests=$test_count shards=$shard_count list_sha256=$test_list_sha256 output=$work_dir"
