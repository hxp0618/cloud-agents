#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
runner="$script_dir/test-migration-shards.sh"
exact_go=${CLOUD_AGENTS_GO:-/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go}
fixture_root=""
runner_pid=""
fixture_wrapper_pid=""

fail() {
  echo "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n $runner_pid ]] && kill -0 "$runner_pid" 2>/dev/null; then
    kill -TERM "$runner_pid" 2>/dev/null || true
    wait "$runner_pid" 2>/dev/null || true
  fi
  if [[ $fixture_wrapper_pid =~ ^[1-9][0-9]*$ ]]; then
    kill -TERM -- "-$fixture_wrapper_pid" 2>/dev/null || true
    kill -KILL -- "-$fixture_wrapper_pid" 2>/dev/null || true
    kill -TERM "$fixture_wrapper_pid" 2>/dev/null || true
    kill -KILL "$fixture_wrapper_pid" 2>/dev/null || true
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
    mixed)
      if [[ $test_name == TestAlpha ]]; then
        printf '{"Time":"2026-08-22T00:00:01Z","Action":"run","Package":"%s","Test":"%s"}\n' "$package" "$test_name"
        printf '{"Time":"2026-08-22T00:00:02Z","Action":"pass","Package":"%s","Test":"%s","Elapsed":0.001}\n' "$package" "$test_name"
        printf '{"Time":"2026-08-22T00:00:03Z","Action":"pass","Package":"%s","Elapsed":0.001}\n' "$package"
      else
        sleep 300 &
        worker=$!
        printf '%s\t%s\n' "$$" "$worker" >>"${FAKE_PID_FILE:?}"
        wait "$worker"
      fi
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

assert_pid_and_group_gone() {
  local pid=$1
  local attempt=0
  while ((attempt < 50)); do
    if ! kill -0 "$pid" 2>/dev/null && ! kill -0 -- "-$pid" 2>/dev/null; then
      return 0
    fi
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
  [[ $(awk -F '\t' '$1 == "deferred_during_launch" { print $2 }' "$output_dir/run-aborted.tsv") == 0 ]] || fail "$signal_name was unexpectedly deferred during launch"
  [[ $(awk -F '\t' '$1 == "deferred_during_retirement" { print $2 }' "$output_dir/run-aborted.tsv") == 0 ]] || fail "$signal_name was unexpectedly deferred during retirement"
  [[ $(awk -F '\t' '$1 == "process_group_cleanup" { print $2 }' "$output_dir/run-aborted.tsv") == complete ]] || fail "$signal_name cleanup was not complete"
  [[ $(sed -n '1p' "$output_dir/run-status.txt") == ABORTED ]] || fail "$signal_name run status is not ABORTED"
  before_digest=$(artifact_digest "$output_dir")
  sleep 0.5
  after_digest=$(artifact_digest "$output_dir")
  [[ $before_digest == "$after_digest" ]] || fail "$signal_name artifacts changed after runner exit"
}

run_pre_registration_signal_case() {
  local signal_name=$1
  local expected_exit=$2
  local case_root="$fixture_root/pre-registration-$signal_name"
  local output_dir="$case_root/output"
  local pid_file="$case_root/fake-pids.tsv"
  local bash_env="$case_root/bash-env.sh"
  local marker="$case_root/pre-registration-ready.txt"
  local release="$case_root/pre-registration-release.txt"
  local before_digest
  local after_digest
  local status
  mkdir -- "$case_root"
  : >"$pid_file"

  cat >"$bash_env" <<'EOF'
__cag_pause_before_shard_registration() {
  case ${BASH_COMMAND:-} in
    'active_pids+='*)
      trap - DEBUG
      printf '%s\n' "${shard_pid:?}" >"${CAG_PRE_REGISTRATION_MARKER:?}"
      while [[ ! -f ${CAG_PRE_REGISTRATION_RELEASE:?} ]]; do
        sleep 0.01
      done
      ;;
  esac
}
trap '__cag_pause_before_shard_registration' DEBUG
EOF

  set -m
  env BASH_ENV="$bash_env" CAG_PRE_REGISTRATION_MARKER="$marker" CAG_PRE_REGISTRATION_RELEASE="$release" \
    REAL_GO="$exact_go" FAKE_MODE=signal FAKE_PID_FILE="$pid_file" CLOUD_AGENTS_GO="$fake_go" \
    /bin/bash "$runner" run --output-dir "$output_dir" --shards 2 --jobs 2 --test-parallel 1 --timeout 5m \
    >"$case_root/runner.stdout" 2>"$case_root/runner.stderr" &
  runner_pid=$!
  set +m
  wait_for_lines "$marker" 1 || fail "$signal_name pre-registration fixture did not reach the launch window"
  fixture_wrapper_pid=$(sed -n '1p' "$marker")
  [[ $fixture_wrapper_pid =~ ^[1-9][0-9]*$ ]] || fail "$signal_name pre-registration fixture recorded an invalid wrapper PID"
  kill -"$signal_name" "$runner_pid"
  sleep 0.1
  : >"$release"
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  runner_pid=""
  [[ $status == "$expected_exit" ]] || fail "$signal_name pre-registration runner exit=$status expected=$expected_exit"
  assert_pid_and_group_gone "$fixture_wrapper_pid" || fail "$signal_name pre-registration signal left a wrapper or process group alive"
  fixture_wrapper_pid=""
  [[ ! -s $pid_file ]] || fail "$signal_name pre-registration fixture unexpectedly started a fake worker"
  [[ $(awk -F '\t' '$1 == "status" { print $2 }' "$output_dir/run-aborted.tsv") == ABORTED ]] || fail "$signal_name pre-registration fixture missing aborted record"
  [[ $(awk -F '\t' '$1 == "signal" { print $2 }' "$output_dir/run-aborted.tsv") == "$signal_name" ]] || fail "$signal_name pre-registration signal record mismatch"
  [[ $(awk -F '\t' '$1 == "deferred_during_launch" { print $2 }' "$output_dir/run-aborted.tsv") == 1 ]] || fail "$signal_name was not deferred during launch"
  [[ $(awk -F '\t' '$1 == "deferred_during_retirement" { print $2 }' "$output_dir/run-aborted.tsv") == 0 ]] || fail "$signal_name pre-registration signal was unexpectedly deferred during retirement"
  [[ $(awk -F '\t' '$1 == "process_group_cleanup" { print $2 }' "$output_dir/run-aborted.tsv") == complete ]] || fail "$signal_name pre-registration cleanup was not complete"
  [[ $(sed -n '1p' "$output_dir/run-status.txt") == ABORTED ]] || fail "$signal_name pre-registration run status is not ABORTED"
  before_digest=$(artifact_digest "$output_dir")
  sleep 0.5
  after_digest=$(artifact_digest "$output_dir")
  [[ $before_digest == "$after_digest" ]] || fail "$signal_name pre-registration artifacts changed after runner exit"
}

run_registered_unexpected_exit_case() {
  local case_root="$fixture_root/registered-unexpected-exit"
  local output_dir="$case_root/output"
  local pid_file="$case_root/fake-pids.tsv"
  local bash_env="$case_root/bash-env.sh"
  local marker="$case_root/registered-wrapper-pid.txt"
  local before_digest
  local after_digest
  local status
  mkdir -- "$case_root"
  : >"$pid_file"

  cat >"$bash_env" <<'EOF'
__cag_exit_after_shard_registration() {
  case ${BASH_COMMAND:-} in
    'launch_in_progress=0')
      if [[ ${run_in_progress:-0} == 1 ]] && ((${#active_pids[@]} > 0)); then
        trap - DEBUG
        printf '%s\n' "${shard_pid:?}" >"${CAG_REGISTERED_EXIT_MARKER:?}"
        exit 97
      fi
      ;;
  esac
}
trap '__cag_exit_after_shard_registration' DEBUG
EOF

  set -m
  env BASH_ENV="$bash_env" CAG_REGISTERED_EXIT_MARKER="$marker" \
    REAL_GO="$exact_go" FAKE_MODE=signal FAKE_PID_FILE="$pid_file" CLOUD_AGENTS_GO="$fake_go" \
    /bin/bash "$runner" run --output-dir "$output_dir" --shards 2 --jobs 2 --test-parallel 1 --timeout 5m \
    >"$case_root/runner.stdout" 2>"$case_root/runner.stderr" &
  runner_pid=$!
  set +m
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  runner_pid=""
  [[ $status == 97 ]] || fail "registered unexpected-exit runner exit=$status expected=97"
  wait_for_lines "$marker" 1 || fail "registered unexpected-exit fixture did not reach the launch window"
  fixture_wrapper_pid=$(sed -n '1p' "$marker")
  [[ $fixture_wrapper_pid =~ ^[1-9][0-9]*$ ]] || fail "registered unexpected-exit fixture recorded an invalid wrapper PID"
  assert_pid_and_group_gone "$fixture_wrapper_pid" || fail "registered unexpected exit left a wrapper or process group alive"
  fixture_wrapper_pid=""
  [[ ! -s $pid_file ]] || fail "registered unexpected-exit fixture unexpectedly started a fake worker"
  [[ ! -f $output_dir/run-status.txt || $(sed -n '1p' "$output_dir/run-status.txt") != PASS ]] || fail "registered unexpected exit published PASS"
  before_digest=$(artifact_digest "$output_dir")
  sleep 0.5
  after_digest=$(artifact_digest "$output_dir")
  [[ $before_digest == "$after_digest" ]] || fail "registered unexpected-exit artifacts changed after runner exit"
}

run_retirement_signal_case() {
  local case_root="$fixture_root/retirement-signal"
  local output_dir="$case_root/output"
  local pid_file="$case_root/fake-pids.tsv"
  local bash_env="$case_root/bash-env.sh"
  local marker="$case_root/retirement-ready.txt"
  local release="$case_root/retirement-release.txt"
  local kill_log="$case_root/runner-kill.tsv"
  local retired_pgid
  local before_digest
  local after_digest
  local status
  mkdir -- "$case_root"
  : >"$pid_file"
  : >"$kill_log"

  cat >"$bash_env" <<'EOF'
kill() {
  local argument
  printf 'kill' >>"${CAG_KILL_LOG:?}"
  for argument in "$@"; do
    printf '\t%s' "$argument" >>"${CAG_KILL_LOG:?}"
  done
  printf '\n' >>"${CAG_KILL_LOG:?}"
  builtin kill "$@"
}

__cag_pause_before_shard_retirement() {
  case ${BASH_COMMAND:-} in
    'active_states[batch_offset]=retired')
      trap - DEBUG
      printf '%s\n' "${shard_pgid:?}" >"${CAG_RETIREMENT_MARKER:?}"
      while [[ ! -f ${CAG_RETIREMENT_RELEASE:?} ]]; do
        sleep 0.01
      done
      ;;
  esac
}
trap '__cag_pause_before_shard_retirement' DEBUG
EOF

  set -m
  env BASH_ENV="$bash_env" CAG_KILL_LOG="$kill_log" CAG_RETIREMENT_MARKER="$marker" CAG_RETIREMENT_RELEASE="$release" \
    REAL_GO="$exact_go" FAKE_MODE=mixed FAKE_PID_FILE="$pid_file" CLOUD_AGENTS_GO="$fake_go" \
    /bin/bash "$runner" run --output-dir "$output_dir" --shards 2 --jobs 2 --test-parallel 1 --timeout 5m \
    >"$case_root/runner.stdout" 2>"$case_root/runner.stderr" &
  runner_pid=$!
  set +m
  wait_for_lines "$marker" 1 || fail "retirement fixture did not reach the retirement window"
  wait_for_lines "$pid_file" 1 || fail "retirement fixture slow worker did not start"
  retired_pgid=$(sed -n '1p' "$marker")
  [[ $retired_pgid =~ ^[1-9][0-9]*$ ]] || fail "retirement fixture recorded an invalid retired PGID"
  : >"$kill_log"
  kill -TERM "$runner_pid"
  sleep 0.1
  : >"$release"
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  runner_pid=""
  [[ $status == 143 ]] || fail "retirement signal runner exit=$status expected=143"
  if awk -F '\t' -v target="-$retired_pgid" '{ for (field = 2; field <= NF; field++) if ($field == target) found = 1 } END { exit found ? 0 : 1 }' "$kill_log"; then
    fail "retirement signal cleanup touched the retired PGID"
  fi
  assert_processes_gone "$pid_file" "$output_dir" || fail "retirement signal left a worker or active process group alive"
  [[ $(awk -F '\t' '$1 == "status" { print $2 }' "$output_dir/run-aborted.tsv") == ABORTED ]] || fail "retirement signal missing aborted record"
  [[ $(awk -F '\t' '$1 == "deferred_during_launch" { print $2 }' "$output_dir/run-aborted.tsv") == 0 ]] || fail "retirement signal was unexpectedly deferred during launch"
  [[ $(awk -F '\t' '$1 == "deferred_during_retirement" { print $2 }' "$output_dir/run-aborted.tsv") == 1 ]] || fail "retirement signal was not deferred during retirement"
  [[ $(awk -F '\t' '$1 == "process_group_cleanup" { print $2 }' "$output_dir/run-aborted.tsv") == complete ]] || fail "retirement signal cleanup was not complete"
  before_digest=$(artifact_digest "$output_dir")
  sleep 0.5
  after_digest=$(artifact_digest "$output_dir")
  [[ $before_digest == "$after_digest" ]] || fail "retirement signal artifacts changed after runner exit"
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
run_pre_registration_signal_case TERM 143
run_pre_registration_signal_case INT 130
run_registered_unexpected_exit_case
run_retirement_signal_case

echo "Migration shard runner fixture: PASS"
