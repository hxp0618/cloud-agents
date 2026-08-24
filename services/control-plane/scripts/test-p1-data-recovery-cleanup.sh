#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
finish_library="$script_dir/p1-data-recovery-cleanup.sh"

run_fixture() {
  local cleanup_status=$1
  CLEANUP_STATUS="$cleanup_status" /bin/bash -c '
    set -u
    source "$1"
    cleanup() {
      printf "CLEANUP\n"
      return "$CLEANUP_STATUS"
    }
    trap "printf EXIT_TRAP_RAN\\n" EXIT
    p1_data_recovery_finish PASS
  ' bash "$finish_library"
}

success_output=$(run_fixture 0)
if [[ $success_output != $'CLEANUP\nPASS' ]]; then
  echo "successful cleanup did not precede PASS: $success_output" >&2
  exit 1
fi

set +e
failure_output=$(run_fixture 1 2>&1)
failure_status=$?
set -e
if [[ $failure_status -eq 0 ]]; then
  echo "cleanup failure returned success" >&2
  exit 1
fi
if [[ $failure_output != "CLEANUP" || $failure_output == *PASS* ]]; then
  echo "cleanup failure published output: $failure_output" >&2
  exit 1
fi

echo "p1-data-recovery-cleanup-fixture: PASS"
