#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
runner="$script_dir/test-migration-shards.sh"
exact_go=${CLOUD_AGENTS_GO:-/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go}
fixture_root=""
runner_pid=""

fail() {
  echo "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n $runner_pid ]] && kill -0 "$runner_pid" 2>/dev/null; then
    kill -TERM "$runner_pid" 2>/dev/null || true
    wait "$runner_pid" 2>/dev/null || true
  fi
  if [[ -n $fixture_root && -d $fixture_root ]]; then
    find "$fixture_root" -depth -delete
  fi
}

trap cleanup EXIT INT TERM

[[ -x $exact_go ]] || fail "exact Go binary is not executable: $exact_go"
[[ $("$exact_go" version | awk '{print $3}') == go1.26.6 ]] || fail "fixture requires Go 1.26.6"
[[ -z $(git -C "$repo_root" status --porcelain=v1 --untracked-files=all) ]] || fail "fixture requires a clean worktree"
/bin/bash -n "$runner"

fixture_root=$(mktemp -d "${TMPDIR:-/tmp}/cag-migration-shard-runner-fixture.XXXXXX")
fake_go="$fixture_root/fake-go"
cat >"$fake_go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == version ]]; then
  echo 'go version go1.26.6 darwin/arm64'
  exit 0
fi

is_test=0
is_list=0
is_json=0
regex=''
previous=''
for argument in "$@"; do
  if [[ $argument == test ]]; then
    is_test=1
  elif [[ $argument == -list ]]; then
    is_list=1
  elif [[ $argument == -json ]]; then
    is_json=1
  elif [[ $previous == -run ]]; then
    regex=$argument
  fi
  previous=$argument
done

if ((is_test == 1 && is_list == 1)); then
  printf 'TestAlpha\nTestBeta\n'
  exit 0
fi

if ((is_test == 1 && is_json == 1)); then
  package='github.com/hxp0618/cloud-agents/services/control-plane/internal/migration'
  test_name=''
  case "$regex" in
    *TestAlpha*) test_name=TestAlpha ;;
    *TestBeta*) test_name=TestBeta ;;
    *) exit 17 ;;
  esac
  printf '{"Time":"2026-08-22T00:00:00Z","Action":"start","Package":"%s"}\n' "$package"
  case "${FAKE_MODE:?}" in
    valid)
      printf '{"Time":"2026-08-22T00:00:01Z","Action":"run","Package":"%s","Test":"%s"}\n' "$package" "$test_name"
      printf '{"Time":"2026-08-22T00:00:02Z","Action":"pass","Package":"%s","Test":"%s","Elapsed":0.001}\n' "$package" "$test_name"
      printf '{"Time":"2026-08-22T00:00:03Z","Action":"pass","Package":"%s","Elapsed":0.001}\n' "$package"
      ;;
    missing)
      printf '{"Time":"2026-08-22T00:00:03Z","Action":"pass","Package":"%s","Elapsed":0.001}\n' "$package"
      ;;
    signal)
      sleep 300 &
      worker=$!
      printf '%s\t%s\n' "$$" "$worker" >>"${FAKE_PID_FILE:?}"
      wait "$worker"
      ;;
    *) exit 18 ;;
  esac
  exit 0
fi

exec "${REAL_GO:?}" "$@"
EOF
chmod 0700 "$fake_go"

artifact_digest() {
  local directory=$1
  (
    cd -- "$directory"
    find . -type f -print | LC_ALL=C sort | while IFS= read -r file; do
      shasum -a 256 "$file"
    done
  ) | shasum -a 256 | awk '{print $1}'
}

wait_for_lines() {
  local file=$1
  local expected=$2
  local attempt=0
  local observed=0
  while ((attempt < 200)); do
    if [[ -f $file ]]; then
      observed=$(wc -l <"$file" | tr -d ' ')
      ((observed >= expected)) && return 0
    fi
    sleep 0.05
    attempt=$((attempt + 1))
  done
  return 1
}

assert_processes_gone() {
  local pid_file=$1
  local output_dir=$2
  local attempt=0
  local alive=1
  while ((attempt < 50)); do
    alive=0
    while IFS=$'\t' read -r shell_pid child_pid; do
      if kill -0 "$shell_pid" 2>/dev/null || kill -0 "$child_pid" 2>/dev/null; then
        alive=1
      fi
    done <"$pid_file"
    while IFS=$'\t' read -r key value; do
      if [[ $key == process_group_id ]] && kill -0 -- "-$value" 2>/dev/null; then
        alive=1
      fi
    done < <(find "$output_dir" -name process-group.tsv -type f -exec cat {} \;)
    ((alive == 0)) && return 0
    sleep 0.05
    attempt=$((attempt + 1))
  done
  return 1
}

run_signal_case() {
  local signal_name=$1
  local expected_exit=$2
  local case_root="$fixture_root/signal-$signal_name"
  local output_dir="$case_root/output"
  local pid_file="$case_root/fake-pids.tsv"
  local before_digest
  local after_digest
  local status
  mkdir -- "$case_root"
  : >"$pid_file"

  set -m
  env REAL_GO="$exact_go" FAKE_MODE=signal FAKE_PID_FILE="$pid_file" CLOUD_AGENTS_GO="$fake_go" \
    /bin/bash "$runner" run --output-dir "$output_dir" --shards 2 --jobs 2 --test-parallel 1 --timeout 5m \
    >"$case_root/runner.stdout" 2>"$case_root/runner.stderr" &
  runner_pid=$!
  set +m
  wait_for_lines "$pid_file" 2 || fail "$signal_name fixture workers did not start"
  kill -"$signal_name" "$runner_pid"
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  runner_pid=""
  [[ $status == "$expected_exit" ]] || fail "$signal_name runner exit=$status expected=$expected_exit"
  assert_processes_gone "$pid_file" "$output_dir" || fail "$signal_name left a worker or process group alive"
  [[ $(awk -F '\t' '$1 == "status" { print $2 }' "$output_dir/run-aborted.tsv") == ABORTED ]] || fail "$signal_name missing aborted record"
  [[ $(awk -F '\t' '$1 == "signal" { print $2 }' "$output_dir/run-aborted.tsv") == "$signal_name" ]] || fail "$signal_name record mismatch"
  [[ $(awk -F '\t' '$1 == "process_group_cleanup" { print $2 }' "$output_dir/run-aborted.tsv") == complete ]] || fail "$signal_name cleanup was not complete"
  [[ $(sed -n '1p' "$output_dir/run-status.txt") == ABORTED ]] || fail "$signal_name run status is not ABORTED"
  before_digest=$(artifact_digest "$output_dir")
  sleep 0.5
  after_digest=$(artifact_digest "$output_dir")
  [[ $before_digest == "$after_digest" ]] || fail "$signal_name artifacts changed after runner exit"
}

run_result_case() {
  local fake_mode=$1
  local expected_exit=$2
  local expected_status=$3
  local case_root="$fixture_root/result-$fake_mode"
  local output_dir="$case_root/output"
  local status
  mkdir -- "$case_root"
  env REAL_GO="$exact_go" FAKE_MODE="$fake_mode" FAKE_PID_FILE="$case_root/unused.tsv" CLOUD_AGENTS_GO="$fake_go" \
    /bin/bash "$runner" run --output-dir "$output_dir" --shards 2 --jobs 2 --test-parallel 1 --timeout 1m \
    >"$case_root/runner.stdout" 2>"$case_root/runner.stderr" &
  runner_pid=$!
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  runner_pid=""
  [[ $status == "$expected_exit" ]] || fail "$fake_mode runner exit=$status expected=$expected_exit"
  [[ $(sed -n '1p' "$output_dir/run-status.txt") == "$expected_status" ]] || fail "$fake_mode run status mismatch"
  if [[ $fake_mode == valid ]]; then
    [[ $(awk -F '\t' 'NR > 1 && $2 == 0 && $4 == 1 && $5 == 1 && $6 == 1 && $8 == 0 && $9 == 1 { count++ } END { print count+0 }' "$output_dir/validation.tsv") == 2 ]] || fail "valid result did not close both shards"
  else
    [[ ! -s $output_dir/shard-00/validation.tsv ]] || fail "missing-test fixture unexpectedly minted validation PASS"
  fi
}

run_result_case valid 0 PASS
run_result_case missing 1 FAIL
run_signal_case TERM 143
run_signal_case INT 130

echo "Migration shard runner fixture: PASS"
