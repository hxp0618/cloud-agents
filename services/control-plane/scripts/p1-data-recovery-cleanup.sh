#!/usr/bin/env bash

# The caller owns cleanup(). A successful runner result is publishable only
# after that cleanup has completed; an EXIT trap cannot carry this contract
# because Bash preserves the status that triggered the trap.
p1_data_recovery_finish() {
  if [[ $# -ne 1 ]]; then
    return 64
  fi
  local pass_line=$1
  trap - EXIT
  if ! cleanup; then
    return 1
  fi
  printf '%s\n' "$pass_line"
}
