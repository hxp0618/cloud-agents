#!/bin/bash
set -euo pipefail

readonly WRAPPER_POLICY="VERSIONED_ISOLATION_WRAPPER_V1"
readonly PROJECTION_ARCHIVE_NAME="core-generator-input-projection.tar"
readonly ROOTFS_TAR_SHA256="25ecc117cd77a289cc25006605dcf4ec8b137fec326db766d0abcd4147f6093e"
readonly ROOTFS_TAR_SIZE_BYTES="80669696"
readonly RUN_TIMEOUT_SECONDS="3600"
readonly RUNNER_TIMEOUT_SECONDS="$((RUN_TIMEOUT_SECONDS - 60))"
readonly MAX_CAPTURE_BYTES="1048576"
readonly TRUSTED_PYTHON="/usr/bin/python3"
readonly TRUSTED_GIT="/usr/bin/git"
readonly FINAL_REVIEW_PATH="docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md"
# Bootstrap authority: this is checked before the first archive-inspector
# invocation.  Update it only when the inspector itself is reviewed and the
# resulting wrapper/registry lineage is regenerated.
readonly ARCHIVE_INSPECTOR_SHA256="sha256:db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31"
readonly PROJECTION_EXCLUSIONS=(
  "contracts/generation.lock.json"
  "tools/generator-supply/v1/evidence-manifest.json"
  "tools/generator-supply/v1/profile.json"
  "tools/generator-supply/v1/evidence/replay.json"
  "tools/generator-supply/v1/evidence/replay/darwin-a.json"
  "tools/generator-supply/v1/evidence/replay/darwin-b.json"
  "tools/generator-supply/v1/evidence/replay/darwin-isolation.json"
  "tools/generator-supply/v1/evidence/replay/linux-a.json"
  "tools/generator-supply/v1/evidence/replay/linux-b.json"
  "tools/generator-supply/v1/evidence/replay/linux-isolation.json"
  "tools/generator-supply/v1/evidence/replay/projection.json"
  "tools/generator-supply/v1/evidence/replay/rejected-executor.json"
  "$FINAL_REVIEW_PATH"
)
readonly GENERATOR_OUTPUT_FILES=(
  "contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json"
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json"
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json"
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json"
  "contracts/generated/platform/v1alpha1/durable-coordination-registry.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-abort-terminal-writer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-ambiguous-resolution-writer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-commit-observation-writer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-recovery-admission-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-recovery-execution-admission-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-recovery-success-writer-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-retry-handoff-registry-v1.json"
  "contracts/generated/platform/v1alpha1/runner-ledger-return-failure-registry-v1.json"
  "contracts/generated/proto/cloud-agents-v1alpha1.binpb"
  "contracts/generated/proto/manifest.json"
  "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platform_adapter.pb.go"
  "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platformadapterv1alpha1connect/platform_adapter.connect.go"
  "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go"
  "sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go"
  "sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go"
  "sdk/go/gen/common/v1alpha1/identity_generated.go"
  "sdk/go/gen/common/v1alpha1/json_generated.go"
  "sdk/go/gen/openapi/v1alpha1/client_generated.go"
  "sdk/go/gen/platform/v1alpha1/json_generated.go"
  "sdk/go/generated-manifest.json"
  "sdk/go/json-generated-manifest.json"
  "sdk/go/proto-generated-manifest.json"
  "sdk/typescript/generated-manifest.json"
  "sdk/typescript/json-generated-manifest.json"
  "sdk/typescript/proto-generated-manifest.json"
  "sdk/typescript/src/gen/contracts/platform-adapter/v1alpha1/platform_adapter_pb.ts"
  "sdk/typescript/src/gen/contracts/worker/v1alpha1/kernel_pb.ts"
  "sdk/typescript/src/gen/contracts/worker/v1alpha1/worker_supervisor_pb.ts"
  "sdk/typescript/src/index.ts"
  "sdk/typescript/src/platform.ts"
  "sdk/typescript/src/proto.ts"
  "services/control-plane/internal/compatibility/registry_generated.go"
  "services/control-plane/internal/coordination/registry_generated.go"
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go"
  "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go"
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go"
  "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go"
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go"
)

fail() {
  printf 'generator replay isolation wrapper: %s\n' "$*" >&2
  exit 1
}

require_absolute() {
  [[ "$2" = /* ]] || fail "$1 must be absolute"
}

require_canonical_regular_file() {
  local name="$1" path="$2" parent canonical
  require_absolute "$name" "$path"
  [[ -f "$path" && ! -L "$path" ]] || fail "$name must be a regular non-symlink file"
  parent="$(cd "$(dirname "$path")" && /bin/pwd -P)"
  canonical="$parent/$(basename "$path")"
  [[ "$canonical" == "$path" ]] || fail "$name must be canonical"
}

require_canonical_directory() {
  local name="$1" path="$2" canonical
  require_absolute "$name" "$path"
  [[ -d "$path" && ! -L "$path" ]] || fail "$name must be a regular non-symlink directory"
  canonical="$(cd "$path" && /bin/pwd -P)"
  [[ "$canonical" == "$path" ]] || fail "$name must be canonical"
}

require_fresh_canonical_leaf() {
  local name="$1" path="$2" parent base expected
  require_absolute "$name" "$path"
  case "$path" in
    */..|*/../*) fail "$name must not contain lexical .." ;;
  esac
  parent="${path%/*}"
  [[ -n "$parent" ]] || parent="/"
  base="${path##*/}"
  [[ -n "$base" && "$base" != "." && "$base" != ".." ]] ||
    fail "$name basename must not be . or .."
  require_canonical_directory "$name-parent" "$parent"
  if [[ "$parent" == "/" ]]; then
    expected="/$base"
  else
    expected="$parent/$base"
  fi
  [[ "$path" == "$expected" ]] || fail "$name must equal canonical-parent/basename"
  [[ ! -e "$path" && ! -L "$path" ]] || fail "$name must initially be absent"
}

require_profile_safe_path() {
  [[ "$2" =~ ^[A-Za-z0-9_./:-]+$ ]] || fail "$1 contains sandbox profile metacharacters"
}

emit_profile_metadata_chain() {
  local current="$1"
  while [[ "$current" != "/" ]]; do
    printf '(allow file-read-metadata (literal "%s"))\n' "$current"
    current="$(/usr/bin/dirname "$current")"
  done
}

trusted_git() {
  /usr/bin/env -i PATH=/usr/bin:/bin HOME=/var/empty LC_ALL=C LANG=C TZ=UTC \
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_NO_REPLACE_OBJECTS=1 \
    GIT_OPTIONAL_LOCKS=0 "$TRUSTED_GIT" "$@"
}

trusted_git_index() {
  local index="$1"
  shift
  /usr/bin/env -i PATH=/usr/bin:/bin HOME=/var/empty LC_ALL=C LANG=C TZ=UTC \
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_NO_REPLACE_OBJECTS=1 \
    GIT_OPTIONAL_LOCKS=0 GIT_INDEX_FILE="$index" "$TRUSTED_GIT" "$@"
}

sha256_file() {
  if [[ -n "${PYTHON:-}" && -x "$PYTHON" ]]; then
    "$PYTHON" -c '
import hashlib
import pathlib
import sys

print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest())
' "$1"
  elif [[ -x /usr/bin/sha256sum ]]; then
    /usr/bin/sha256sum "$1" | /usr/bin/awk '{print $1}'
  elif [[ -x /usr/bin/shasum ]]; then
    /usr/bin/shasum -a 256 "$1" | /usr/bin/awk '{print $1}'
  else
    fail "neither /usr/bin/sha256sum nor /usr/bin/shasum is available"
  fi
}

symlink_identity() {
  "$TRUSTED_PYTHON" -c '
import json
import os
import stat
import sys

path = sys.argv[1]
metadata = os.lstat(path)
if not stat.S_ISLNK(metadata.st_mode):
    raise SystemExit("expected one symbolic link")
print(json.dumps({
    "device": metadata.st_dev,
    "inode": metadata.st_ino,
    "mode": metadata.st_mode,
    "target": os.readlink(path),
}, sort_keys=True, separators=(",", ":")))
' "$1"
}

file_size() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    /usr/bin/stat -f %z "$1"
  else
    /usr/bin/stat -c %s "$1"
  fi
}

wrapper_path() {
  local directory
  directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && /bin/pwd -P)"
  printf '%s/%s\n' "$directory" "$(basename "${BASH_SOURCE[0]}")"
}

authority_runner_path() {
  printf '%s/replay-platform-generators.ts\n' "$(cd "$(dirname "$(wrapper_path)")" && /bin/pwd -P)"
}

authority_path_helper_path() {
  printf '%s/lib/generator-replay-path-authority.ts\n' "$(cd "$(dirname "$(wrapper_path)")" && /bin/pwd -P)"
}

authority_archive_inspector_path() {
  archive_inspector
}

compute_authority_digests() {
  local wrapper="$1" runner helper inspector
  runner="$(authority_runner_path)"
  helper="$(authority_path_helper_path)"
  inspector="$(authority_archive_inspector_path)"
  require_canonical_regular_file authority-wrapper "$wrapper"
  require_canonical_regular_file authority-runner "$runner"
  require_canonical_regular_file authority-path-helper "$helper"
  require_canonical_regular_file authority-archive-inspector "$inspector"
  [[ "sha256:$(sha256_file "$inspector")" == "$ARCHIVE_INSPECTOR_SHA256" ]] ||
    fail "archive inspector bootstrap digest drifted"
  AUTHORITY_WRAPPER_SHA256="sha256:$(sha256_file "$wrapper")"
  AUTHORITY_RUNNER_SHA256="sha256:$(sha256_file "$runner")"
  AUTHORITY_PATH_HELPER_SHA256="sha256:$(sha256_file "$helper")"
  AUTHORITY_ARCHIVE_INSPECTOR_SHA256="sha256:$(sha256_file "$inspector")"
}

verify_authority_digests() {
  [[ "$#" -eq 6 ]] || fail "verify-authority-digests requires wrapper root and four digests"
  local wrapper="$1" root="$2" wrapper_sha="$3" runner_sha="$4" helper_sha="$5" inspector_sha="$6"
  local runner="$root/scripts/replay-platform-generators.ts"
  local helper="$root/scripts/lib/generator-replay-path-authority.ts"
  local inspector="$root/scripts/lib/inspect-generator-replay-archive.py"
  require_canonical_regular_file child-wrapper "$wrapper"
  require_canonical_regular_file child-runner "$runner"
  require_canonical_regular_file child-path-helper "$helper"
  require_canonical_regular_file child-archive-inspector "$inspector"
  [[ "sha256:$(sha256_file "$wrapper")" == "$wrapper_sha" ]] || fail "child wrapper digest drifted"
  [[ "sha256:$(sha256_file "$runner")" == "$runner_sha" ]] || fail "child runner digest drifted"
  [[ "sha256:$(sha256_file "$helper")" == "$helper_sha" ]] || fail "child path helper digest drifted"
  [[ "sha256:$(sha256_file "$inspector")" == "$inspector_sha" ]] || fail "child archive inspector digest drifted"
}

archive_inspector() {
  printf '%s/lib/inspect-generator-replay-archive.py\n' "$(cd "$(dirname "${BASH_SOURCE[0]}")" && /bin/pwd -P)"
}

inspect_archive() {
  local profile="$1" archive="$2" output="$3" inspector
  inspector="$(archive_inspector)"
  require_canonical_regular_file archive-inspector "$inspector"
  [[ "sha256:$(sha256_file "$inspector")" == "$ARCHIVE_INSPECTOR_SHA256" ]] ||
    fail "archive inspector drifted before trusted preflight"
  [[ ! -e "$output" && ! -L "$output" ]] || fail "archive inspection output must initially be absent"
  /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C LANG=C TZ=UTC \
    "$TRUSTED_PYTHON" "$inspector" "$profile" "$archive" >"$output"
  [[ -s "$output" ]] || fail "archive inspection output is empty"
}

validate_projection_snapshot() {
  [[ "$#" -eq 4 ]] || fail "validate-projection-snapshot requires archive metadata tree inspection"
  local archive="$1" metadata="$2" tree="$3" inspection="$4" archive_sha archive_size
  require_canonical_regular_file projection-snapshot-archive "$archive"
  require_canonical_regular_file projection-snapshot-metadata "$metadata"
  require_canonical_regular_file projection-snapshot-inspection "$inspection"
  archive_sha="sha256:$(sha256_file "$archive")"
  archive_size="$(file_size "$archive")"
  "$TRUSTED_PYTHON" - "$metadata" "$inspection" "$tree" "$archive_sha" "$archive_size" <<'PY'
import json
import sys

metadata_path, inspection_path, tree, archive_sha, archive_size = sys.argv[1:]
with open(metadata_path, encoding="utf-8") as source:
    metadata = json.load(source)
with open(inspection_path, encoding="utf-8") as source:
    inspection = json.load(source)
if metadata != {
    "formatVersion": "cloud-agents-core-generator-projection/v1",
    "treeSha": tree,
    "archiveSha256": archive_sha,
    "archiveSizeBytes": int(archive_size),
    "archiveInspection": inspection,
    "excluded": metadata.get("excluded"),
}:
    raise SystemExit("projection snapshot metadata is not an exact archive binding")
if metadata.get("excluded") != [
    "contracts/generation.lock.json",
    "tools/generator-supply/v1/evidence-manifest.json",
    "tools/generator-supply/v1/profile.json",
    "tools/generator-supply/v1/evidence/replay.json",
    "tools/generator-supply/v1/evidence/replay/darwin-a.json",
    "tools/generator-supply/v1/evidence/replay/darwin-b.json",
    "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
    "tools/generator-supply/v1/evidence/replay/linux-a.json",
    "tools/generator-supply/v1/evidence/replay/linux-b.json",
    "tools/generator-supply/v1/evidence/replay/linux-isolation.json",
    "tools/generator-supply/v1/evidence/replay/projection.json",
    "tools/generator-supply/v1/evidence/replay/rejected-executor.json",
    "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
]:
    raise SystemExit("projection snapshot exclusion authority drifted")
if inspection.get("profile") != "core-projection" or inspection.get("reconstructedGitTreeSha") != tree:
    raise SystemExit("projection snapshot inspection tree binding drifted")
PY
}

snapshot_file() {
  [[ "$#" -eq 3 ]] || fail "snapshot-file requires source destination label"
  local source="$1" destination="$2" label="$3"
  require_canonical_regular_file "$label-source" "$source"
  [[ ! -e "$destination" && ! -L "$destination" ]] || fail "$label snapshot destination must be absent"
  /bin/cp "$source" "$destination"
  require_canonical_regular_file "$label-snapshot" "$destination"
  [[ "$(sha256_file "$source")" == "$(sha256_file "$destination")" ]] || fail "$label snapshot digest drifted"
  [[ "$(file_size "$source")" == "$(file_size "$destination")" ]] || fail "$label snapshot size drifted"
}

build_projection() {
  [[ "$#" -eq 3 ]] || fail "build-projection requires <repository> <output-directory>"
  local repository="$2" output="$3" index full_tree tree archive archive_sha inspection
  require_canonical_regular_file trusted-git "$TRUSTED_GIT"
  [[ -x "$TRUSTED_GIT" ]] || fail "trusted Git executable is unavailable"
  require_absolute repository "$repository"
  require_fresh_canonical_leaf projection-output "$output"
  repository="$(cd "$repository" && /bin/pwd -P)"
  trusted_git -C "$repository" diff --quiet || fail "projection requires no unstaged tracked changes"
  [[ -z "$(trusted_git -C "$repository" ls-files --others --exclude-standard)" ]] ||
    fail "projection requires no untracked files"
  /bin/mkdir -m 0700 "$output"
  index="$output/projection.index"
  full_tree="$(trusted_git -C "$repository" write-tree)"
  [[ "$full_tree" =~ ^[0-9a-f]{40}$ ]] || fail "full staged Git tree SHA is invalid"
  trusted_git_index "$index" -C "$repository" read-tree "$full_tree"
  for exclusion in "${PROJECTION_EXCLUSIONS[@]}"; do
    while IFS= read -r -d '' path; do
      trusted_git_index "$index" -C "$repository" update-index --force-remove -- "$path"
    done < <(trusted_git_index "$index" -C "$repository" ls-files -z -- "$exclusion")
  done
  while IFS= read -r -d '' record; do
    [[ "$record" != *$'\n'* ]] || fail "projection path contains a newline"
    case "${record%% *}" in
      100644|100755) ;;
      *) fail "projection rejects non-regular Git mode in $record" ;;
    esac
  done < <(trusted_git_index "$index" -C "$repository" ls-files --stage -z)
  tree="$(trusted_git_index "$index" -C "$repository" write-tree)"
  [[ "$tree" =~ ^[0-9a-f]{40}$ ]] || fail "projection tree SHA is invalid"
  archive="$output/$PROJECTION_ARCHIVE_NAME"
  trusted_git_index "$index" -c tar.umask=0022 -C "$repository" archive --format=tar \
    --mtime=1970-01-01T00:00:00Z --output="$archive" "$tree"
  [[ "$(trusted_git -C "$repository" write-tree)" == "$full_tree" ]] ||
    fail "staged Git index changed while building projection"
  /bin/rm "$index"
  inspection="$output/projection-inspection.json"
  inspect_archive core-projection "$archive" "$inspection"
  [[ "$(/usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reconstructedGitTreeSha"])' "$inspection")" == "$tree" ]] ||
    fail "projection archive does not reconstruct the exact staged Git tree"
  archive_sha="$(sha256_file "$archive")"
  /usr/bin/python3 - "$output/projection.json" "$tree" "sha256:$archive_sha" "$(file_size "$archive")" "$inspection" <<'PY'
import json
import os
import sys

output, tree, archive_sha, archive_size, inspection_path = sys.argv[1:]
if os.path.lexists(output):
    raise SystemExit("projection metadata output must initially be absent")
with open(inspection_path, encoding="utf-8") as source:
    inspection = json.load(source)
document = {
    "formatVersion": "cloud-agents-core-generator-projection/v1",
    "treeSha": tree,
    "archiveSha256": archive_sha,
    "archiveSizeBytes": int(archive_size),
    "archiveInspection": inspection,
    "excluded": [
        "contracts/generation.lock.json",
        "tools/generator-supply/v1/evidence-manifest.json",
        "tools/generator-supply/v1/profile.json",
        "tools/generator-supply/v1/evidence/replay.json",
        "tools/generator-supply/v1/evidence/replay/darwin-a.json",
        "tools/generator-supply/v1/evidence/replay/darwin-b.json",
        "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
        "tools/generator-supply/v1/evidence/replay/linux-a.json",
        "tools/generator-supply/v1/evidence/replay/linux-b.json",
        "tools/generator-supply/v1/evidence/replay/linux-isolation.json",
        "tools/generator-supply/v1/evidence/replay/projection.json",
        "tools/generator-supply/v1/evidence/replay/rejected-executor.json",
        "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
    ],
}
with open(output, "x", encoding="utf-8") as destination:
    json.dump(document, destination, sort_keys=True, separators=(",", ":"))
    destination.write("\n")
PY
  /bin/rm "$inspection"
}

tool_paths() {
  local platform="$1" supply="$2"
  if [[ "$platform" == "darwin-arm64" ]]; then
    NODE="$supply/toolchains/darwin-arm64/node/bin/node"
    BUN="$supply/toolchains/darwin-arm64/bun/bun-darwin-aarch64/bun"
    GO="$supply/toolchains/darwin-arm64/go/bin/go"
    GOFMT="$supply/toolchains/darwin-arm64/go/bin/gofmt"
    PYTHON="$supply/toolchains/darwin-arm64/python/bin/python3.14"
    UV="$supply/toolchains/darwin-arm64/uv/uv"
    PROTOC="$supply/toolchains/darwin-arm64/protoc/bin/protoc"
    PROTOC_GEN_GO="$supply/plugins/darwin-arm64/protoc-gen-go"
    PROTOC_GEN_CONNECT_GO="$supply/plugins/darwin-arm64/protoc-gen-connect-go"
    NODE_MODULES="$supply/npm-node-24.18.1-darwin/node_modules"
    WHEELHOUSE="$supply/wheelhouse/darwin-arm64"
  else
    NODE="$supply/toolchains/linux-amd64/node/bin/node"
    BUN="$supply/toolchains/linux-amd64/bun/bun-linux-x64-baseline/bun"
    GO="$supply/toolchains/linux-amd64/go/bin/go"
    GOFMT="$supply/toolchains/linux-amd64/go/bin/gofmt"
    PYTHON="$supply/toolchains/linux-amd64/python/bin/python3.14"
    UV="$supply/toolchains/linux-amd64/uv/uv"
    PROTOC="$supply/toolchains/linux-amd64/protoc/bin/protoc"
    PROTOC_GEN_GO="$supply/plugins/linux-amd64/protoc-gen-go"
    PROTOC_GEN_CONNECT_GO="$supply/plugins/linux-amd64/protoc-gen-connect-go"
    NODE_MODULES="$supply/npm-linux-glibc/node_modules"
    WHEELHOUSE="$supply/wheelhouse/linux-amd64"
  fi
  for path in "$NODE" "$BUN" "$GO" "$GOFMT" "$PYTHON" "$UV" "$PROTOC" \
    "$PROTOC_GEN_GO" "$PROTOC_GEN_CONNECT_GO"; do
    [[ -f "$path" && ! -L "$path" && -x "$path" ]] || fail "missing exact executable $path"
  done
  [[ -d "$NODE_MODULES" && ! -L "$NODE_MODULES" ]] || fail "missing node_modules closure"
  [[ -d "$WHEELHOUSE" && ! -L "$WHEELHOUSE" ]] || fail "missing wheelhouse closure"
  TOOL_PATH="$(dirname "$NODE"):$(dirname "$BUN"):$(dirname "$GO"):$(dirname "$PYTHON"):$(dirname "$UV"):$(dirname "$PROTOC"):$(dirname "$PROTOC_GEN_GO"):/usr/bin:/bin"
}

validate_supply_executables() {
  [[ "$#" -eq 2 ]] || fail "validate-supply-executables requires platform and supply root"
  local platform="$1" supply="$2"
  "$TRUSTED_PYTHON" -c '
import hashlib
import os
import pathlib
import stat
import sys

platform, supply = sys.argv[1:]
expected = {
    "darwin-arm64": {
        "node": ("toolchains/darwin-arm64/node/bin/node", "f480e325ee0ca9cb9eef00b5ca6057a2a104807a1b073f1bc373a55c67facff5", 120957760),
        "bun": ("toolchains/darwin-arm64/bun/bun-darwin-aarch64/bun", "e0c90ec15d33363e6b70713d56bc3b2c7585c17f40a0fe0f8fd9305901d4e233", 63096576),
        "go": ("toolchains/darwin-arm64/go/bin/go", "a1c83801d1756c3eca78366c6b585f2c21c20694fb1c7eb92c446a0580420412", 14516816),
        "gofmt": ("toolchains/darwin-arm64/go/bin/gofmt", "350abb0daa1b58fa1fc7a48dcd482f75ebccfc9b7252f1113ef7585ad6245911", 2970400),
        "python": ("toolchains/darwin-arm64/python/bin/python3.14", "c8a41706db062af069cf32b388a29eb2879b810d2d8dd4a5fd5bda3d163132ac", 18800624),
        "uv": ("toolchains/darwin-arm64/uv/uv", "ad3564874e19defa0debefcf48e8381ac1d087c584190c1323c247bd351dd25f", 42201264),
        "protoc": ("toolchains/darwin-arm64/protoc/bin/protoc", "d81c6d9240f7eb8d18d41c6abc9cd6ca7896e370c7b575adfcf2fa1d600727de", 9575824),
        "protoc-gen-go": ("plugins/darwin-arm64/protoc-gen-go", "2812f5f5cc1f08dff81fdbeb66b6c1ce642da13587c05801900cfca5f4230939", 7528018),
        "protoc-gen-connect-go": ("plugins/darwin-arm64/protoc-gen-connect-go", "0de128625c8350e1489e85e8483fe68af098ea53682f7ae567571159c00a0642", 12944706),
    },
    "linux-amd64": {
        "node": ("toolchains/linux-amd64/node/bin/node", "f3432a45b03b2da0d270095fdd8813dc34cbea73f5fc8b18c7a384b7cf9b333a", 123656816),
        "bun": ("toolchains/linux-amd64/bun/bun-linux-x64-baseline/bun", "a8f9ebd1770ddc8e55dab7a68d4ec1ec1eebf374bb97cc65cf2c3cb373fc6791", 91802480),
        "go": ("toolchains/linux-amd64/go/bin/go", "29e6e0b8be61beb1489ceae62b304343566de8a1dc700af74bde7aeb9c80ad45", 15434687),
        "gofmt": ("toolchains/linux-amd64/go/bin/gofmt", "0ef6fe2d15c972d15b8ecb75dcb9c861e042ac94a0e48baf0202a840648b0fdc", 3110743),
        "python": ("toolchains/linux-amd64/python/bin/python3.14", "4f43b25b39a5db0144b949fef5587aac333be2f4ae5f793a479ccb424cf7c22f", 32062496),
        "uv": ("toolchains/linux-amd64/uv/uv", "b65f23a420c4acc96427efb30e5ed9bc0f7e25d2d712000f6ede77c1a0de5f46", 59019440),
        "protoc": ("toolchains/linux-amd64/protoc/bin/protoc", "d2c65c3ea5eeb59427f684ed3e0a0cd458386122fc9f1b98de946a7242b53d31", 10488040),
        "protoc-gen-go": ("plugins/linux-amd64/protoc-gen-go", "72e151a7861ff00dc1e229c084efa1db3d52f8349b8d6249f3f232879ddb3f57", 7700525),
        "protoc-gen-connect-go": ("plugins/linux-amd64/protoc-gen-connect-go", "70ae4ff2362596582685c0cb41905c525ed8b5d13ab48ad24987394ced22995b", 13359247),
    },
}
for name, (relative, expected_sha, expected_size) in expected[platform].items():
    path = pathlib.Path(supply, relative)
    if not path.is_file() or path.is_symlink() or not os.access(path, os.X_OK):
        raise SystemExit(f"supply executable {name} is not a regular executable")
    data = path.read_bytes()
    if len(data) != expected_size or hashlib.sha256(data).hexdigest() != expected_sha:
        raise SystemExit(f"supply executable {name} bytes drifted before child")
' "$platform" "$supply"
}

run_one() {
  local platform="$1" run="$2" root="$3" run_authority="$4" archive="$5" metadata="$6" tree="$7"
  local wrapper_sha="$8" runner_sha="$9" helper_sha="${10}" inspector_sha="${11}"
  local lower status
  lower="$(printf '%s' "$run" | /usr/bin/tr '[:upper:]' '[:lower:]')"
  /bin/mkdir "$run_authority/tmp-$lower"
  if [[ "$platform" == "linux-amd64" ]]; then
    # The trusted runner stays uid 0, while every generator child is uid 65534.
    # Make only this exact pre-created TMPDIR candidate-owned; HOME/UV/XDG stay
    # fresh and are created by the candidate below the 1777 ephemeral parent.
    /usr/bin/chown 65534:65534 "$run_authority/tmp-$lower"
    /bin/chmod 0700 "$run_authority/tmp-$lower"
  fi
  set +e
  /usr/bin/env -i \
    PATH="$TOOL_PATH" HOME="$run_authority/home-$lower" TMPDIR="$run_authority/tmp-$lower" \
    UV_CACHE_DIR="$run_authority/uv-cache-$lower" XDG_CACHE_HOME="$run_authority/xdg-cache-$lower" \
    UV_NO_CONFIG=1 LC_ALL=C LANG=C TZ=UTC GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=readonly NO_COLOR=1 \
    CLOUD_AGENTS_GENERATOR_PLATFORM="$platform" CLOUD_AGENTS_GENERATOR_REPLAY_RUN="$run" \
    CLOUD_AGENTS_GENERATOR_RUNNER_ENVIRONMENT_POLICY=ENV_I_MINIMAL_V1 \
    CLOUD_AGENTS_GENERATOR_WRAPPER_POLICY="$WRAPPER_POLICY" \
    CLOUD_AGENTS_GENERATOR_WRAPPER_SHA256="$wrapper_sha" \
    CLOUD_AGENTS_GENERATOR_RUNNER_SHA256="$runner_sha" \
    CLOUD_AGENTS_GENERATOR_PATH_HELPER_SHA256="$helper_sha" \
    CLOUD_AGENTS_GENERATOR_ARCHIVE_INSPECTOR_SHA256="$inspector_sha" \
    CLOUD_AGENTS_GENERATOR_NODE_MODULES_BIND_MODE="${NODE_MODULES_BIND_MODE:-EXTERNAL_SYMLINK_V1}" \
    CLOUD_AGENTS_GENERATOR_RUN_ROOT="$run_authority" \
    CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE="$archive" \
    CLOUD_AGENTS_GENERATOR_PROJECTION_ARCHIVE_SHA256="sha256:$(sha256_file "$archive")" \
    CLOUD_AGENTS_GENERATOR_PROJECTION_METADATA="$metadata" \
    CLOUD_AGENTS_GENERATOR_PROJECTION_TREE_SHA="$tree" \
    CLOUD_AGENTS_NODE="$NODE" CLOUD_AGENTS_BUN="$BUN" CLOUD_AGENTS_GO="$GO" \
    CLOUD_AGENTS_GOFMT="$GOFMT" CLOUD_AGENTS_PYTHON="$PYTHON" CLOUD_AGENTS_UV="$UV" \
    CLOUD_AGENTS_PROTOC="$PROTOC" CLOUD_AGENTS_PROTOC_GEN_GO="$PROTOC_GEN_GO" \
    CLOUD_AGENTS_PROTOC_GEN_CONNECT_GO="$PROTOC_GEN_CONNECT_GO" \
    CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE="$WHEELHOUSE" \
    NODE_PROBE="$NODE_PROBE" PYTHON_PROBE="$PYTHON_PROBE" SUPPLY_PROBE="$SUPPLY_PROBE" \
    ARCHIVE_PROBE="$ARCHIVE_PROBE" ROOTFS_PROBE="${ROOTFS_PROBE:-}" INPUT_PROBE="${INPUT_PROBE:-}" \
    PROJECTION_PROBE="${PROJECTION_PROBE:-}" ROUTE_PROBE="${ROUTE_PROBE:-}" \
    IDENTITY_PROBE="${IDENTITY_PROBE:-}" NODE_MODULES_PROBE="${NODE_MODULES_PROBE:-}" \
    NODE_MODULES_RELINK_PROBE="${NODE_MODULES_RELINK_PROBE:-}" \
    STDOUT_PROBE="${STDOUT_PROBE:-}" DETACHED_PROBE="${DETACHED_PROBE:-}" \
    POSIX_SPAWN_PROBE="${POSIX_SPAWN_PROBE:-}" SIBLING_PROBE="$SIBLING_PROBE" FINAL_PROBE="$FINAL_PROBE" \
    NODE_EXIT="$NODE_EXIT" PYTHON_EXIT="$PYTHON_EXIT" SUPPLY_EXIT="$SUPPLY_EXIT" \
    ARCHIVE_EXIT="$ARCHIVE_EXIT" ROOTFS_EXIT="${ROOTFS_EXIT:-0}" INPUT_EXIT="${INPUT_EXIT:-0}" \
    PROJECTION_EXIT="${PROJECTION_EXIT:-0}" ROUTE_EXIT="${ROUTE_EXIT:-0}" \
    IDENTITY_EXIT="${IDENTITY_EXIT:-0}" NODE_MODULES_EXIT="${NODE_MODULES_EXIT:-0}" \
    NODE_MODULES_RELINK_EXIT="${NODE_MODULES_RELINK_EXIT:-0}" \
    STDOUT_EXIT="${STDOUT_EXIT:-0}" DETACHED_EXIT="${DETACHED_EXIT:-0}" \
    POSIX_SPAWN_EXIT="${POSIX_SPAWN_EXIT:-0}" SIBLING_EXIT="$SIBLING_EXIT" FINAL_EXIT="$FINAL_EXIT" \
    "$PYTHON" -c '
import json
import os
import selectors
import signal
import subprocess
import sys
import time

platform, replay_run, root, bun, max_bytes, timeout = sys.argv[1:]
max_bytes = int(max_bytes)
timeout = int(timeout)
probe_names = {
    "NODE", "PYTHON", "SUPPLY", "ARCHIVE", "ROOTFS", "INPUT", "PROJECTION", "ROUTE",
    "IDENTITY", "NODE_MODULES", "NODE_MODULES_RELINK", "STDOUT", "DETACHED", "POSIX_SPAWN", "SIBLING", "FINAL",
}
probe_env = {name: os.environ.get(f"{name}_PROBE", "") for name in probe_names}
exit_env = {name: int(os.environ.get(f"{name}_EXIT", "0")) for name in probe_names}
child_env = {
    key: value for key, value in os.environ.items()
    if not any(key == f"{name}_PROBE" or key == f"{name}_EXIT" for name in probe_names)
}
process = subprocess.Popen(
    [bun, f"{root}/scripts/replay-platform-generators.ts", "--run", root],
    cwd=root,
    env=child_env,
    stdin=subprocess.DEVNULL,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    close_fds=True,
    start_new_session=True,
)
assert process.stdout is not None and process.stderr is not None
selector = selectors.DefaultSelector()
selector.register(process.stdout, selectors.EVENT_READ, "stdout")
selector.register(process.stderr, selectors.EVENT_READ, "stderr")
streams = {"stdout": bytearray(), "stderr": bytearray()}
deadline = time.monotonic() + timeout
while True:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
        raise SystemExit("generator replay runner exceeded outer timeout")
    for key, _ in selector.select(min(remaining, 1.0)):
        chunk = os.read(key.fd, 65536)
        if chunk:
            streams[key.data].extend(chunk)
            if len(streams[key.data]) > max_bytes:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise SystemExit("generator replay runner output exceeded v1 bound")
        else:
            selector.unregister(key.fileobj)
    if process.poll() is not None and not selector.get_map():
        break
runner_stdout = bytes(streams["stdout"])
runner_stderr = bytes(streams["stderr"])
if process.returncode != 0:
    sys.stderr.buffer.write(runner_stderr)
    raise SystemExit(f"generator replay runner failed with status {process.returncode}")
try:
    runner_frame = runner_stdout.decode("utf-8")
except UnicodeDecodeError as error:
    raise SystemExit(f"generator replay runner stdout is not UTF-8: {error}") from error

def probe(name, command):
    return {"command": command, "exitCode": exit_env[name], "stdout": "", "stderr": probe_env[name]}

document = {
    "formatVersion": "cloud-agents-generator-isolated-run/v1",
    "platform": platform,
    "replayRun": replay_run,
    "mechanism": "SANDBOX_EXEC_DENY_DEFAULT_PER_RUN_BOUNDARY_V1" if platform == "darwin-arm64" else "UNSHARE_NET_MOUNT_PID_FRESH_ROOTFS_SETPRIV_V1",
    "runnerFrame": runner_frame,
    "runnerStderrBytes": len(runner_stderr),
    "probes": {
        "node": probe("NODE", "node net.connect 1.1.1.1:443"),
        "python": probe("PYTHON", "python socket.connect 1.1.1.1:443"),
        "supply": probe("SUPPLY", "touch generator-supply://external-supply/codex-generator-supply-read-only-probe"),
        "archive": probe("ARCHIVE", "touch generator-supply://core-projection/archive"),
        "nodeModules": probe("NODE_MODULES", "unlink or write the bound node_modules authority"),
        "sibling": probe("SIBLING", "test sibling replay root absent"),
        "final": probe("FINAL", "read trusted-parent final evidence sentinel"),
    },
}
if platform == "darwin-arm64":
    document["probes"].update({
        "nodeModulesRelink": probe("NODE_MODULES_RELINK", "replace the bound node_modules symlink"),
        "detachedDescendant": probe("DETACHED", "fork setsid fork then read trusted-parent sentinel"),
        "posixSpawnDetached": probe("POSIX_SPAWN", "posix_spawn setsid child then read trusted-parent sentinel"),
    })
else:
    document["probes"].update({
        "stdoutChannel": probe("STDOUT", "read /proc/1/fd/1 trusted runner stdout channel"),
        "rootfs": probe("ROOTFS", "touch /etc/codex-generator-supply-read-only-probe"),
        "input": probe("INPUT", "touch /input/codex-generator-supply-read-only-probe"),
        "projection": probe("PROJECTION", "touch /projection/core-generator-input-projection.tar"),
        "route": probe("ROUTE", "awk default route /proc/net/route"),
        "identity": {
            "command": "read uid gid groups capabilities and no-new-privileges",
            "exitCode": exit_env["IDENTITY"],
            "stdout": probe_env["IDENTITY"],
            "stderr": "",
        },
    })
payload = json.dumps(document, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
if not payload or len(payload) > max_bytes:
    raise SystemExit("isolated run envelope exceeds v1 bound")
sys.stdout.buffer.write(
    b"CLOUD_AGENTS_GENERATOR_ISOLATED_RUN_V1 " + str(len(payload)).encode("ascii") + b"\n" + payload + b"\n"
)
' "$platform" "$run" "$root" "$BUN" "$MAX_CAPTURE_BYTES" "$RUNNER_TIMEOUT_SECONDS"
  status=$?
  set -e
  [[ "$status" -eq 0 ]] || fail "generator replay runner failed for $platform/$run"
}

inner_darwin_run() {
  [[ "$#" -eq 14 ]] || fail "inner-darwin-run requires run archive metadata tree root authority supply sibling final wrapper and authority digests"
  local run="$2" archive="$3" metadata="$4" tree="$5" root="$6" authority="$7" supply="$8" sibling="$9" final="${10}"
  local wrapper_sha="${11}" runner_sha="${12}" helper_sha="${13}" inspector_sha="${14}"
  tool_paths darwin-arm64 "$supply"
  verify_authority_digests "$(wrapper_path)" "$root" "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
  set +e
  NODE_PROBE="$("$NODE" -e 'const n=require("node:net");const s=n.connect(443,"1.1.1.1");s.on("connect",()=>process.exit(0));s.on("error",e=>{console.error(`${e.code}:${e.message}`);process.exit(1)})' 2>&1)"; NODE_EXIT=$?
  PYTHON_PROBE="$("$PYTHON" -c 'import socket; s=socket.socket(); s.settimeout(2); s.connect(("1.1.1.1",443))' 2>&1)"; PYTHON_EXIT=$?
  SUPPLY_PROBE="$(cd "$supply" && /usr/bin/touch codex-generator-supply-read-only-probe 2>&1)"; SUPPLY_EXIT=$?
  ARCHIVE_PROBE="$(cd "$(dirname "$archive")" && /usr/bin/touch "$(basename "$archive")" 2>&1)"; ARCHIVE_EXIT=$?
  NODE_MODULES_PROBE="$(cd "$root" && "$PYTHON" -c '
import errno
import os
import sys

try:
    os.unlink("node_modules")
except OSError as error:
    print(f"errno={error.errno}", flush=True)
    sys.exit(1 if error.errno == errno.EPERM else 2)
print("unlink succeeded", flush=True)
sys.exit(0)
' )"; NODE_MODULES_EXIT=$?
  NODE_MODULES_RELINK_PROBE="$("$PYTHON" -c '
import errno
import os
import sys

path, temporary = sys.argv[1:]
try:
    os.replace(temporary, path)
except OSError as error:
    print(f"errno={error.errno}", flush=True)
    sys.exit(1 if error.errno == errno.EPERM else 2)
print("relink succeeded", flush=True)
sys.exit(0)
' "$root/node_modules" "$authority/node-modules-relink")"; NODE_MODULES_RELINK_EXIT=$?
  SIBLING_PROBE="$(test ! -e "$sibling" 2>&1)"; SIBLING_EXIT=$?
  FINAL_PROBE="$(/bin/cat "$final" 2>&1)"; FINAL_EXIT=$?
  DETACHED_PROBE="$("$PYTHON" -c '
import errno
import os
import sys

path = sys.argv[1]
child = os.fork()
if child == 0:
    os.setsid()
    grandchild = os.fork()
    if grandchild == 0:
        try:
            os.open(path, os.O_RDONLY)
        except OSError as error:
            print(f"errno={error.errno}", flush=True)
            os._exit(0 if error.errno == errno.EPERM else 1)
        os._exit(1)
    _, status = os.waitpid(grandchild, 0)
    os._exit(os.waitstatus_to_exitcode(status))
_, status = os.waitpid(child, 0)
sys.exit(os.waitstatus_to_exitcode(status))
' "$final")"; DETACHED_EXIT=$?
  POSIX_SPAWN_PROBE="$("$PYTHON" -c '
import errno
import os
import sys

path = sys.argv[1]
code = """import errno,os,sys
p=sys.argv[1]
try:
    os.open(p, os.O_RDONLY)
except OSError as error:
    print(f"errno={error.errno}", flush=True)
    sys.exit(0 if error.errno == errno.EPERM else 1)
sys.exit(1)
"""
try:
    pid = os.posix_spawn(sys.executable, [sys.executable, "-c", code, path], os.environ.copy(), setsid=True)
except (AttributeError, TypeError) as error:
    print(f"unsupported={error}")
    sys.exit(1)
_, status = os.waitpid(pid, 0)
sys.exit(os.waitstatus_to_exitcode(status))
' "$final")"; POSIX_SPAWN_EXIT=$?
  set -e
  FINAL_PROBE="${FINAL_PROBE//$final/generator-supply:\/\/trusted-parent\/final-evidence-sentinel}"
  [[ "$NODE_EXIT" -ne 0 && "$PYTHON_EXIT" -ne 0 && "$SUPPLY_EXIT" -ne 0 && "$ARCHIVE_EXIT" -ne 0 && \
    "$NODE_MODULES_EXIT" -eq 1 && "$NODE_MODULES_RELINK_EXIT" -eq 1 && \
    "$SIBLING_EXIT" -eq 0 && "$FINAL_EXIT" -ne 0 && \
    "$DETACHED_EXIT" -eq 0 && "$POSIX_SPAWN_EXIT" -eq 0 ]] ||
    fail "Darwin isolation probes did not fail closed"
  [[ "$NODE_PROBE" == *EPERM* && "$PYTHON_PROBE" == *"Operation not permitted"* && \
    "$SUPPLY_PROBE" == *"Operation not permitted"* && "$ARCHIVE_PROBE" == *"Operation not permitted"* && \
    "$NODE_MODULES_PROBE" == "errno=1" && "$NODE_MODULES_RELINK_PROBE" == "errno=1" && \
    "$FINAL_PROBE" == *"Operation not permitted"* && "$DETACHED_PROBE" == "errno=1" && \
    "$POSIX_SPAWN_PROBE" == "errno=1" ]] || fail "Darwin isolation probe errors drifted"
  run_one darwin-arm64 "$run" "$root" "$authority" "$archive" "$metadata" "$tree" \
    "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
  verify_authority_digests "$(wrapper_path)" "$root" "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
}

supervise_darwin_run() {
  local capture="$1"; shift
  [[ ! -e "$capture" && ! -L "$capture" ]] || fail "Darwin capture path must initially be absent"
  [[ ! -e "$capture.stderr" && ! -L "$capture.stderr" ]] || fail "Darwin stderr path must initially be absent"
  "$TRUSTED_PYTHON" - "$capture" "$RUN_TIMEOUT_SECONDS" "$MAX_CAPTURE_BYTES" "$@" <<'PY'
import os
import selectors
import signal
import subprocess
import sys
import time

capture, timeout, max_bytes, *command = sys.argv[1:]
deadline = time.monotonic() + int(timeout)
process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, close_fds=True, start_new_session=True)
assert process.stdout is not None and process.stderr is not None
selector = selectors.DefaultSelector()
selector.register(process.stdout, selectors.EVENT_READ, "stdout")
selector.register(process.stderr, selectors.EVENT_READ, "stderr")
streams = {"stdout": bytearray(), "stderr": bytearray()}
while True:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
        raise SystemExit("isolated Darwin run exceeded outer timeout")
    for key, _ in selector.select(min(remaining, 1.0)):
        chunk = os.read(key.fd, 65536)
        if chunk:
            body = streams[key.data]
            body.extend(chunk)
            if len(body) > int(max_bytes):
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise SystemExit(f"isolated Darwin {key.data} exceeded v1 bound")
        else:
            selector.unregister(key.fileobj)
    if process.poll() is not None and not selector.get_map():
        break
status = process.wait()
with open(capture, "xb") as destination:
    destination.write(streams["stdout"])
with open(capture + ".stderr", "xb") as destination:
    destination.write(streams["stderr"])
if status != 0:
    sys.stderr.buffer.write(streams["stderr"])
    raise SystemExit(f"isolated Darwin run failed with status {status}")
PY
  [[ "$(file_size "$capture")" -le "$MAX_CAPTURE_BYTES" ]] || fail "Darwin isolated stdout exceeded v1 bound"
  [[ "$(file_size "$capture.stderr")" -le "$MAX_CAPTURE_BYTES" ]] || fail "Darwin isolated stderr exceeded v1 bound"
}

parse_run_capture() {
  local capture="$1" report="$2" platform="$3" run="$4"
  [[ ! -e "$report" && ! -L "$report" ]] || fail "trusted-parent report path must initially be absent"
  "$TRUSTED_PYTHON" - "$capture" "$report" "$platform" "$run" <<'PY'
import json
import pathlib
import re
import sys

capture, report_path, platform, replay_run = sys.argv[1:]

def parse_frame(raw: bytes, prefix: bytes):
    first = raw.find(b"\n")
    if first < 0 or not raw.endswith(b"\n"):
        raise SystemExit(f"{prefix.decode()} is incomplete")
    header = raw[:first]
    match = re.fullmatch(prefix + rb" ([1-9][0-9]*)", header)
    if match is None:
        raise SystemExit(f"{prefix.decode()} header is invalid")
    payload = raw[first + 1:-1]
    expected = int(match.group(1))
    if expected > 1024 * 1024 or len(payload) != expected or b"\n" in payload:
        raise SystemExit(f"{prefix.decode()} size or trailing bytes are invalid")
    try:
        value = json.loads(payload.decode("utf-8"))
    except Exception as error:
        raise SystemExit(f"{prefix.decode()} payload is not JSON: {error}") from error
    if not isinstance(value, dict):
        raise SystemExit(f"{prefix.decode()} payload must be one object")
    return value

envelope = parse_frame(pathlib.Path(capture).read_bytes(), b"CLOUD_AGENTS_GENERATOR_ISOLATED_RUN_V1")
if envelope.get("platform") != platform or envelope.get("replayRun") != replay_run:
    raise SystemExit("isolated run envelope platform/run drifted")
runner_frame = envelope.get("runnerFrame")
if not isinstance(runner_frame, str):
    raise SystemExit("isolated run runner frame is not a string")
report = parse_frame(runner_frame.encode("utf-8"), b"CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1")
if report.get("platform") != platform or report.get("replayRun") != replay_run:
    raise SystemExit("runner report platform/run drifted")
serialized = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
for forbidden in ("/private/tmp/", "/tmp/", "/var/tmp/", "/var/folders/", "/Users/huang/"):
    if forbidden in serialized:
        raise SystemExit(f"runner report retained host path {forbidden}")
path = pathlib.Path(report_path)
if path.exists() or path.is_symlink():
    raise SystemExit("trusted-parent report path must initially be absent")
with path.open("x", encoding="utf-8") as destination:
    destination.write(serialized)
PY
}

write_isolation_receipt() {
  local platform="$1" output="$2" capture_a="$3" capture_b="$4" wrapper="$5" archive="$6" rootfs_inspection="${7:-}"
  "$TRUSTED_PYTHON" - "$platform" "$output" "$capture_a" "$capture_b" "$wrapper" "$archive" "$rootfs_inspection" <<'PY'
import hashlib
import json
import pathlib
import re
import sys

platform, output, capture_a, capture_b, wrapper, archive, rootfs_inspection_path = sys.argv[1:]
prefix = "darwin" if platform == "darwin-arm64" else "linux"
MAX_BYTES = 1024 * 1024

def fail(message):
    raise SystemExit(message)

def sha(path):
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()

def parse_frame(raw, prefix_bytes):
    first = raw.find(b"\n")
    if first < 0 or not raw.endswith(b"\n"):
        fail(prefix_bytes.decode() + " is incomplete")
    match = re.fullmatch(prefix_bytes + rb" ([1-9][0-9]*)", raw[:first])
    if match is None:
        fail(prefix_bytes.decode() + " header is invalid")
    payload = raw[first + 1:-1]
    if int(match.group(1)) > MAX_BYTES or len(payload) != int(match.group(1)) or b"\n" in payload:
        fail(prefix_bytes.decode() + " size or trailing bytes are invalid")
    try:
        value = json.loads(payload.decode("utf-8"))
    except Exception as error:
        fail(prefix_bytes.decode() + " payload is not JSON: " + str(error))
    if not isinstance(value, dict):
        fail(prefix_bytes.decode() + " payload must be one object")
    return value

def read_json(path):
    with open(path, encoding="utf-8") as source:
        value = json.load(source)
    if not isinstance(value, dict):
        fail("JSON authority must be one object")
    return value

envelopes = [
    parse_frame(pathlib.Path(capture_a).read_bytes(), b"CLOUD_AGENTS_GENERATOR_ISOLATED_RUN_V1"),
    parse_frame(pathlib.Path(capture_b).read_bytes(), b"CLOUD_AGENTS_GENERATOR_ISOLATED_RUN_V1"),
]
reports = [read_json(pathlib.Path(output) / f"{prefix}-{run}.json") for run in ("a", "b")]
mechanism = {
    "darwin-arm64": "SANDBOX_EXEC_DENY_DEFAULT_PER_RUN_BOUNDARY_V1",
    "linux-amd64": "UNSHARE_NET_MOUNT_PID_FRESH_ROOTFS_SETPRIV_V1",
}[platform]

def require_probe(probes, name, *, exit_code=None):
    probe = probes.get(name)
    if not isinstance(probe, dict) or not isinstance(probe.get("command"), str):
        fail(f"nested probe {name} is absent")
    if not isinstance(probe.get("exitCode"), int) or isinstance(probe.get("exitCode"), bool):
        fail(f"nested probe {name} exit code is invalid")
    if not isinstance(probe.get("stdout"), str) or not isinstance(probe.get("stderr"), str):
        fail(f"nested probe {name} output is invalid")
    if exit_code is not None and probe["exitCode"] != exit_code:
        fail(f"nested probe {name} did not have expected exit code")
    return probe

for index, (envelope, report, expected_run) in enumerate(zip(envelopes, reports, ("A", "B"))):
    if envelope.get("formatVersion") != "cloud-agents-generator-isolated-run/v1":
        fail("isolation envelope format drifted")
    if envelope.get("platform") != platform or envelope.get("replayRun") != expected_run:
        fail("isolation envelope platform/run drifted")
    if envelope.get("mechanism") != mechanism:
        fail("isolation envelope mechanism drifted")
    if not isinstance(envelope.get("runnerFrame"), str):
        fail("isolation envelope runner frame is absent")
    if (
        not isinstance(envelope.get("runnerStderrBytes"), int)
        or isinstance(envelope["runnerStderrBytes"], bool)
        or envelope["runnerStderrBytes"] < 0
        or envelope["runnerStderrBytes"] > MAX_BYTES
    ):
        fail("runner stderr capture bound is invalid")
    framed_report = parse_frame(envelope["runnerFrame"].encode("utf-8"), b"CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1")
    if framed_report != report:
        fail("trusted-parent report differs from runner stdout frame")
    probes = envelope.get("probes")
    if not isinstance(probes, dict):
        fail("isolation envelope probes are not nested")
    for name in ("node", "python", "supply", "archive", "nodeModules", "final"):
        require_probe(probes, name)
    require_probe(probes, "sibling", exit_code=0)
    for key in ("formatVersion", "platform", "replayRun", "projectionTreeSha", "projectionArchiveSha256",
                "projectionArchiveSizeBytes", "projectionArchiveMemberManifestAlgorithm",
                "projectionArchiveMemberManifestSha256", "projectionArchiveMembers",
                "inputTreeManifestAlgorithm", "inputTreeManifestSha256", "inputTreeFiles",
                "replayAuthoritySha256", "candidateOutputsEqual", "candidateManifestSha256",
                "replayManifestSha256", "archiveHasGitDirectory", "ambientNodeModules"):
        if key not in report:
            fail(f"runner report missing {key}")
    if report["formatVersion"] != "cloud-agents-generator-replay-run/v1" or report["platform"] != platform or report["replayRun"] != expected_run:
        fail("runner report identity drifted")
    if report["archiveHasGitDirectory"] is not False or report["ambientNodeModules"] is not False:
        fail("runner report archive/node_modules boundary is not closed")
    if report["projectionArchiveMemberManifestAlgorithm"] != "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1":
        fail("runner archive manifest algorithm drifted")
    if report["inputTreeManifestAlgorithm"] != "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1":
        fail("runner input manifest algorithm drifted")
    if report["candidateOutputsEqual"] is not True or report["candidateManifestSha256"] != report["replayManifestSha256"]:
        fail("runner report does not prove candidate output equality")
    if (
        not isinstance(report["projectionArchiveMembers"], int)
        or isinstance(report["projectionArchiveMembers"], bool)
        or report["projectionArchiveMembers"] <= 0
    ):
        fail("runner projection member count authority is absent")
    for digest_name in ("projectionArchiveSha256", "projectionArchiveMemberManifestSha256", "inputTreeManifestSha256",
                        "candidateManifestSha256", "replayManifestSha256"):
        if not isinstance(report[digest_name], str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", report[digest_name]):
            fail(f"runner report digest {digest_name} is invalid")
    if not isinstance(report["projectionTreeSha"], str) or not re.fullmatch(r"[0-9a-f]{40}", report["projectionTreeSha"]):
        fail("runner projection tree authority is invalid")
    if (
        not isinstance(report["projectionArchiveSizeBytes"], int)
        or isinstance(report["projectionArchiveSizeBytes"], bool)
        or report["projectionArchiveSizeBytes"] <= 0
    ):
        fail("runner archive size authority is invalid")
    if (
        not isinstance(report["inputTreeFiles"], int)
        or isinstance(report["inputTreeFiles"], bool)
        or report["inputTreeFiles"] <= 0
    ):
        fail("runner input tree file count is invalid")

if envelopes[0]["mechanism"] != envelopes[1]["mechanism"]:
    fail("A/B isolation mechanism differs")
projection_fields = (
    "projectionTreeSha", "projectionArchiveSha256", "projectionArchiveSizeBytes",
    "projectionArchiveMemberManifestAlgorithm", "projectionArchiveMemberManifestSha256",
    "projectionArchiveMembers", "inputTreeManifestAlgorithm", "inputTreeManifestSha256", "inputTreeFiles",
)
for field in projection_fields:
    if reports[0][field] != reports[1][field]:
        fail("A/B projection authority differs")
archive_sha = sha(archive)
if reports[0]["projectionArchiveSha256"] != archive_sha or reports[0]["projectionArchiveSizeBytes"] != pathlib.Path(archive).stat().st_size:
    fail("report archive digest or size drifted")
authority_dir = pathlib.Path(wrapper).resolve().parent
authority = {
    "wrapper": sha(wrapper),
    "runner": sha(authority_dir / "replay-platform-generators.ts"),
    "pathHelper": sha(authority_dir / "lib/generator-replay-path-authority.ts"),
    "archiveInspector": sha(authority_dir / "lib/inspect-generator-replay-archive.py"),
}
for report in reports:
    if report["replayAuthoritySha256"] != authority:
        fail("runner authority digests do not match trusted pre-child snapshot")
if reports[0]["replayManifestSha256"] != reports[1]["replayManifestSha256"]:
    fail("A/B replay output manifests differ")

for envelope in envelopes:
    probes = envelope["probes"]
    if platform == "linux-amd64":
        for name in ("rootfs", "input", "projection"):
            require_probe(probes, name)
        require_probe(probes, "stdoutChannel")
        require_probe(probes, "route", exit_code=0)
        identity = probes.get("identity")
        if not isinstance(identity, dict) or identity.get("exitCode") != 0 or not isinstance(identity.get("stdout"), str):
            fail("Linux identity probe is incomplete")
        identity_text = identity["stdout"]
        for field, expected in (("Uid", r"65534\s+65534\s+65534\s+65534"), ("Gid", r"65534\s+65534\s+65534\s+65534"),
                                ("Groups", r""), ("CapInh", r"0000000000000000"), ("CapPrm", r"0000000000000000"),
                                ("CapEff", r"0000000000000000"), ("CapBnd", r"0000000000000000"),
                                ("CapAmb", r"0000000000000000"), ("NoNewPrivs", r"1")):
            if re.search(rf"(?m)^{field}:\s*{expected}\s*$", identity_text) is None:
                fail("Linux identity probe is not exact")
        if probes["nodeModules"]["exitCode"] == 0:
            fail("Linux node_modules bind is writable")
        if probes["stdoutChannel"]["exitCode"] == 0:
            fail("Linux candidate can read the trusted runner stdout channel")
    else:
        require_probe(probes, "detachedDescendant")
        require_probe(probes, "posixSpawnDetached")
        require_probe(probes, "nodeModulesRelink", exit_code=1)
        if probes["detachedDescendant"]["exitCode"] != 0 or probes["detachedDescendant"]["stderr"] != "errno=1":
            fail("Darwin detached descendant cross-boundary probe did not fail closed")
        if probes["posixSpawnDetached"]["exitCode"] != 0 or probes["posixSpawnDetached"]["stderr"] != "errno=1":
            fail("Darwin posix_spawn cross-boundary probe did not fail closed")
        if probes["nodeModules"]["exitCode"] != 1 or probes["nodeModules"]["stderr"] != "errno=1":
            fail("Darwin node_modules authority can be unlinked or written")
        if probes["nodeModulesRelink"]["stderr"] != "errno=1":
            fail("Darwin node_modules authority can be relinked")

receipt = {
    "formatVersion": "cloud-agents-generator-replay-isolation/v1",
    "platform": platform,
    "executor": "authorized_darwin_arm64_executor" if platform == "darwin-arm64" else "authorized_linux_amd64_executor",
    "mechanism": mechanism,
    "wrapperPolicy": "VERSIONED_ISOLATION_WRAPPER_V1",
    "boundaryModel": "SEPARATE_RUN_BOUNDARY_STDOUT_TRUSTED_PARENT_V1",
    "wrapperSha256": authority["wrapper"],
    "replayAuthoritySha256": authority,
    "authorityDigestsCapturedBeforeChild": True,
    "authorityFilesReadOnlyToCandidate": True,
    "sameBoundaryProbesAndReplay": True,
    "candidateReportFilesystemAccess": False,
    "reportsWrittenByTrustedParent": True,
    "runnerStdoutFrame": "CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1_LENGTH_PREFIXED_MAX_1M_RAW_FILE",
    "runnerChildStdoutRedirectedToStderr": True,
    "runnerStderrCaptureBoundBytes": MAX_BYTES,
    "runnerEnvironmentPolicy": "ENV_I_MINIMAL_V1",
    "runnerEnvironmentSanitized": True,
    "freshPerReplayCaches": True,
    "extractionRootsInitiallyAbsent": True,
    "archiveSnapshotValidatedBeforeExtraction": True,
    "independentArchiveExtractions": 2,
    "runAWriteRootDestroyedBeforeRunB": True,
    "siblingReplayRootAbsentWithinEachBoundary": True,
    "finalEvidenceUnavailableWithinEachBoundary": True,
    "sameProcessGroupEmptyAfterExit": "NOT_CLAIMED",
    "detachedDescendantCrossBoundaryReadDenied": platform == "darwin-arm64",
    "detachedDescendantsCrossRunReadDenied": False,
    "detachedDescendantResourceLeakNotClaimed": platform == "darwin-arm64",
    "processLifetimeClosure": "NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL" if platform == "darwin-arm64" else "PID_NAMESPACE_KILL_CHILD",
    "projectionTreeSha": reports[0]["projectionTreeSha"],
    "projectionArchiveSha256": reports[0]["projectionArchiveSha256"],
    "projectionArchiveSizeBytes": reports[0]["projectionArchiveSizeBytes"],
    "projectionArchiveMemberManifestAlgorithm": reports[0]["projectionArchiveMemberManifestAlgorithm"],
    "projectionArchiveMemberManifestSha256": reports[0]["projectionArchiveMemberManifestSha256"],
    "projectionArchiveMembers": reports[0]["projectionArchiveMembers"],
    "inputTreeManifestAlgorithm": reports[0]["inputTreeManifestAlgorithm"],
    "inputTreeManifestSha256": reports[0]["inputTreeManifestSha256"],
    "inputTreeFiles": reports[0]["inputTreeFiles"],
    "reportsEqualInputProjection": True,
    "runReportSha256": {"a": sha(pathlib.Path(output) / f"{prefix}-a.json"), "b": sha(pathlib.Path(output) / f"{prefix}-b.json")},
    "probes": {"a": envelopes[0]["probes"], "b": envelopes[1]["probes"]},
    "networkDenied": True,
    "nodeModulesBindingReadOnly": True,
    "notGateClosure": True,
}
if platform == "darwin-arm64":
    receipt.update({
        "denyDefaultSandbox": True,
        "writeAuthorityLimitedToDisposableRunRoot": True,
        "separateProcessGroupPerReplay": True,
        "externalSupplyReadOnly": True,
        "projectionArchiveReadOnly": True,
    })
else:
    rootfs_inspection = read_json(rootfs_inspection_path)
    if rootfs_inspection.get("profile") != "rootfs":
        fail("rootfs inspection profile drifted")
    receipt.update({
        "separateNetworkMountPidNamespacePerReplay": True,
        "pidNamespaceKillsDescendants": True,
        "rootfsFreshExtractionPerReplay": True,
        "rootfsReadOnly": True,
        "inputReadOnly": True,
        "projectionReadOnly": True,
        "tmpfsTmp": True,
        "tmpfsEphemeral": True,
        "tmpfsDeviceTree": True,
        "candidateUid": 65534,
        "candidateGid": 65534,
        "candidateSupplementaryGroups": [],
        "candidateCapabilities": {"effective": "0000000000000000", "permitted": "0000000000000000", "bounding": "0000000000000000", "ambient": "0000000000000000"},
        "noNewPrivileges": True,
        "nodeModulesReadOnlyBind": True,
        "ubuntuRootfs": {
            "registryIndexDigest": "sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517",
            "platformManifestDigest": "sha256:fd225d3a1c5cecb1374f0d09c37a127d1f6f70e665941d6dab888c38b36c2131",
            "configImageId": "sha256:a6f81fb630d51837271b89f8193810a5fc493fa4f30a55d7ebcdb3a66f3cc63a",
            "rootfsLayerDigest": "sha256:b9a65b3c65ab22d490085bd0bf5490e2409da8748b406870f2463bdc41cd6795",
            "exportTarSha256": "25ecc117cd77a289cc25006605dcf4ec8b137fec326db766d0abcd4147f6093e",
            "exportTarSizeBytes": 80669696,
            "archiveInspection": rootfs_inspection,
        },
    })
serialized = json.dumps(receipt, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
for forbidden in ("/private/tmp/", "/tmp/", "/var/tmp/", "/var/folders/", "/Users/huang/"):
    if forbidden in serialized:
        fail("isolation receipt retained host path " + forbidden)
destination = pathlib.Path(output) / f"{prefix}-isolation.json"
if destination.exists() or destination.is_symlink():
    fail("isolation receipt path must initially be absent")
with destination.open("x", encoding="utf-8") as stream:
    stream.write(serialized)
PY
}

run_darwin() {
  [[ "$#" -eq 5 ]] || fail "run-darwin requires <archive> <tree-sha> <task-root> <supply-root>"
  local archive="$2" tree="$3" task="$4" supply="$5"
  local wrapper metadata output capture run lower run_root sibling profile sentinel relink_source readable_root
  local snapshot_archive snapshot_metadata snapshot_inspection node_modules_link authority_wrapper
  local node_modules_identity relink_source_identity
  wrapper="$(wrapper_path)"
  metadata="$(dirname "$archive")/projection.json"
  require_canonical_regular_file projection-archive "$archive"
  require_canonical_regular_file projection-metadata "$metadata"
  require_canonical_regular_file wrapper "$wrapper"
  require_canonical_directory supply "$supply"
  require_fresh_canonical_leaf darwin-task "$task"
  [[ "$tree" =~ ^[0-9a-f]{40}$ ]] || fail "projection tree SHA is invalid"
  for path in "$archive" "$metadata" "$wrapper" "$supply" "$task"; do require_profile_safe_path path "$path"; done
  tool_paths darwin-arm64 "$supply"
  validate_supply_executables darwin-arm64 "$supply"
  compute_authority_digests "$wrapper"
  /bin/mkdir -m 0700 "$task"
  output="$task/output"
  /bin/mkdir -m 0700 "$output" "$task/captures" "$task/profiles" "$task/projection" "$task/authority"
  snapshot_archive="$task/projection/$PROJECTION_ARCHIVE_NAME"
  snapshot_metadata="$task/projection/projection.json"
  snapshot_inspection="$task/projection/inspection.json"
  authority_wrapper="$task/authority/replay-wrapper.sh"
  snapshot_file "$archive" "$snapshot_archive" projection-archive
  snapshot_file "$metadata" "$snapshot_metadata" projection-metadata
  /bin/cp "$wrapper" "$authority_wrapper"
  /bin/chmod 0444 "$authority_wrapper"
  require_canonical_regular_file darwin-authority-wrapper "$authority_wrapper"
  [[ "sha256:$(sha256_file "$authority_wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "Darwin authority wrapper snapshot drifted"
  inspect_archive core-projection "$snapshot_archive" "$snapshot_inspection"
  validate_projection_snapshot "$snapshot_archive" "$snapshot_metadata" "$tree" "$snapshot_inspection"
  sentinel="$output/trusted-parent-sentinel"
  printf 'trusted-parent-only\n' >"$sentinel"
  /bin/chmod 0400 "$sentinel"
  for run in A B; do
    lower="$(printf '%s' "$run" | /usr/bin/tr '[:upper:]' '[:lower:]')"
    run_root="$task/run-$lower"
    sibling="$task/run-$([[ "$run" == A ]] && printf b || printf a)"
    [[ ! -e "$run_root" && ! -L "$run_root" && ! -e "$sibling" && ! -L "$sibling" ]] ||
      fail "Darwin replay roots are not independent and initially absent"
    /bin/mkdir -m 0700 "$run_root"
    /bin/mkdir "$run_root/input" "$run_root/ephemeral"
    /usr/bin/tar -xf "$snapshot_archive" -C "$run_root/input"
    [[ ! -e "$run_root/input/node_modules" && ! -L "$run_root/input/node_modules" ]] ||
      fail "Darwin projection unexpectedly contains node_modules"
    /bin/ln -s "$NODE_MODULES" "$run_root/input/node_modules"
    node_modules_link="$run_root/input/node_modules"
    relink_source="$run_root/ephemeral/node-modules-relink"
    [[ ! -e "$relink_source" && ! -L "$relink_source" ]] || fail "Darwin relink source must initially be absent"
    /bin/ln -s "$supply" "$relink_source"
    node_modules_identity="$(symlink_identity "$node_modules_link")"
    relink_source_identity="$(symlink_identity "$relink_source")"
    for output_file in "${GENERATOR_OUTPUT_FILES[@]}"; do
      [[ -f "$run_root/input/$output_file" && ! -L "$run_root/input/$output_file" ]] ||
        fail "Darwin exact generator output must be an existing regular non-symlink file: $output_file"
    done
    profile="$task/profiles/$lower.sb"
    {
      printf '%s\n' '(version 1)' '(deny default)' '(allow process-fork)' '(allow sysctl-read)' '(allow file-map-executable)'
      printf '%s\n' '(deny mach-lookup)' '(deny network*)'
      printf '(allow process-exec (subpath "%s")\n' "$supply"
      printf '  (literal "%s") (literal "/bin/bash") (literal "/bin/sh") (literal "/usr/bin/env")\n' \
        "$TRUSTED_PYTHON"
      printf '  (literal "%s")\n' "$authority_wrapper"
      printf '  (literal "/bin/cat") (literal "/bin/rm") (literal "/bin/mkdir") (literal "/bin/test") (literal "/usr/bin/touch")\n'
      printf '  (literal "/usr/bin/tr") (literal "/usr/bin/awk") (literal "/usr/bin/grep")\n'
      printf '  (literal "/bin/pwd") (literal "/usr/bin/basename") (literal "/usr/bin/dirname") (literal "/usr/bin/stat")\n'
      printf '  (literal "/usr/bin/shasum") (literal "/bin/echo") )\n'
      printf '(allow file-read-data (literal "/"))\n'
      printf '(allow file-read* (subpath "%s"))\n' "$task/authority"
      for system_path in /System /usr /bin /Library /private/etc /private/var/db; do
        printf '(allow file-read* (subpath "%s"))\n' "$system_path"
      done
      printf '%s\n' '(allow file-read-metadata (literal "/dev"))'
      for device_path in /dev/null /dev/zero /dev/random /dev/urandom; do
        printf '(allow file-read* (literal "%s"))\n' "$device_path"
      done
      for readable_root in "$supply" "$run_root/input" "$run_root/ephemeral" "$task/projection" "$task/authority"; do
        emit_profile_metadata_chain "$readable_root"
      done
      printf '(allow file-read* (subpath "%s"))\n' "$supply"
      printf '(allow file-read* (subpath "%s"))\n' "$run_root/input"
      printf '(allow file-read* (subpath "%s"))\n' "$run_root/ephemeral"
      printf '(allow file-read* (subpath "%s"))\n' "$task/projection"
      printf '(allow file-write* (subpath "%s"))\n' "$run_root/ephemeral"
      printf '%s\n' '(allow file-write* (literal "/dev/null"))'
      for output_file in "${GENERATOR_OUTPUT_FILES[@]}"; do
        printf '(allow file-write* (literal "%s/input/%s"))\n' "$run_root" "$output_file"
      done
      printf '(deny file-write* (literal "%s"))\n' "$node_modules_link"
      printf '(deny file-write* (subpath "%s"))\n' "$node_modules_link"
      printf '(deny file-write* (subpath "%s"))\n' "$NODE_MODULES"
      printf '(deny file-read* (subpath "%s"))\n' "$output"
      printf '(deny file-read* (subpath "%s"))\n' "$sibling"
      printf '(deny file-read* (subpath "%s"))\n' "$sentinel"
      printf '(deny file-write* (subpath "%s"))\n' "$output"
      printf '(deny file-write* (subpath "%s"))\n' "$task/projection"
      printf '(deny file-write* (subpath "%s"))\n' "$task/authority"
    } >"$profile"
    capture="$task/captures/darwin-$lower.frame"
    (
      cd "$run_root/input"
      supervise_darwin_run "$capture" /usr/bin/sandbox-exec -f "$profile" \
        /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C LANG=C TZ=UTC \
        HOME="$run_root/ephemeral" TMPDIR="$run_root/ephemeral" \
        /bin/bash "$authority_wrapper" inner-darwin-run "$run" "$snapshot_archive" "$snapshot_metadata" "$tree" \
        "$run_root/input" "$run_root/ephemeral" "$supply" "$sibling" "$sentinel" \
        "$AUTHORITY_WRAPPER_SHA256" "$AUTHORITY_RUNNER_SHA256" "$AUTHORITY_PATH_HELPER_SHA256" \
        "$AUTHORITY_ARCHIVE_INSPECTOR_SHA256"
    )
    [[ "$(symlink_identity "$node_modules_link")" == "$node_modules_identity" ]] ||
      fail "Darwin node_modules target changed across denied relink probe"
    [[ "$(symlink_identity "$relink_source")" == "$relink_source_identity" ]] ||
      fail "Darwin relink source changed across denied relink probe"
    parse_run_capture "$capture" "$output/darwin-$lower.json" darwin-arm64 "$run"
    /bin/rm -rf "$run_root"
    [[ ! -e "$run_root" && ! -L "$run_root" ]] || fail "Darwin replay root was not destroyed after trusted capture"
  done
  [[ "sha256:$(sha256_file "$wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "wrapper bytes changed across Darwin replay"
  [[ "sha256:$(sha256_file "$authority_wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "Darwin authority wrapper changed across replay"
  [[ "sha256:$(sha256_file "$(authority_runner_path)")" == "$AUTHORITY_RUNNER_SHA256" ]] || fail "runner bytes changed across Darwin replay"
  [[ "sha256:$(sha256_file "$(authority_path_helper_path)")" == "$AUTHORITY_PATH_HELPER_SHA256" ]] || fail "path helper bytes changed across Darwin replay"
  [[ "sha256:$(sha256_file "$(authority_archive_inspector_path)")" == "$AUTHORITY_ARCHIVE_INSPECTOR_SHA256" ]] || fail "archive inspector bytes changed across Darwin replay"
  /bin/rm "$sentinel"
  write_isolation_receipt darwin-arm64 "$output" "$task/captures/darwin-a.frame" \
    "$task/captures/darwin-b.frame" "$wrapper" "$snapshot_archive"
}

candidate_exec() {
  /usr/bin/setpriv --reuid=65534 --regid=65534 --clear-groups \
    --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs -- "$@"
}

inner_linux_run() {
  [[ "$#" -eq 8 ]] || fail "inner-linux-run requires run tree sibling wrapper and authority digests"
  local run="$2" tree="$3" sibling="$4" wrapper_sha="$5" runner_sha="$6" helper_sha="$7" inspector_sha="$8" identity
  NODE_MODULES_BIND_MODE="RO_BIND_MOUNT_V1"
  verify_authority_digests "$(wrapper_path)" /work/input "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
  tool_paths linux-amd64 /input
  set +e
  NODE_PROBE="$(candidate_exec "$NODE" -e 'const n=require("node:net");const s=n.connect(443,"1.1.1.1");s.on("connect",()=>process.exit(0));s.on("error",e=>{console.error(`${e.code}:${e.message}`);process.exit(1)})' 2>&1)"; NODE_EXIT=$?
  PYTHON_PROBE="$(candidate_exec "$PYTHON" -c 'import socket; s=socket.socket(); s.settimeout(2); s.connect(("1.1.1.1",443))' 2>&1)"; PYTHON_EXIT=$?
  SUPPLY_PROBE="$(candidate_exec /usr/bin/touch /input/codex-generator-supply-read-only-probe 2>&1)"; SUPPLY_EXIT=$?
  ARCHIVE_PROBE="$(candidate_exec /usr/bin/touch /projection/$PROJECTION_ARCHIVE_NAME 2>&1)"; ARCHIVE_EXIT=$?
  ROOTFS_PROBE="$(candidate_exec /usr/bin/touch /etc/codex-generator-supply-read-only-probe 2>&1)"; ROOTFS_EXIT=$?
  INPUT_PROBE="$(candidate_exec /usr/bin/touch /input/codex-generator-supply-read-only-probe 2>&1)"; INPUT_EXIT=$?
  PROJECTION_PROBE="$(candidate_exec /usr/bin/touch /projection/core-generator-input-projection.tar 2>&1)"; PROJECTION_EXIT=$?
  NODE_MODULES_PROBE="$(candidate_exec /usr/bin/touch /work/input/node_modules/codex-generator-supply-read-only-probe 2>&1)"; NODE_MODULES_EXIT=$?
  ROUTE_PROBE="$(candidate_exec /usr/bin/awk 'NR > 1 && $2 == "00000000" { print }' /proc/net/route 2>&1)"; ROUTE_EXIT=$?
  identity="$(candidate_exec /usr/bin/awk '/^(Uid|Gid|Groups|CapInh|CapPrm|CapEff|CapBnd|CapAmb|NoNewPrivs):/ { print }' /proc/self/status)"
  IDENTITY_PROBE="$identity"; IDENTITY_EXIT=$?
  STDOUT_PROBE="$(candidate_exec /bin/sh -c '/bin/cat /proc/1/fd/1 >/dev/null' 2>&1)"; STDOUT_EXIT=$?
  SIBLING_PROBE="$(candidate_exec /usr/bin/test ! -e "$sibling" 2>&1)"; SIBLING_EXIT=$?
  FINAL_PROBE="$(candidate_exec /usr/bin/test ! -e /final 2>&1)"; FINAL_EXIT=$?
  set -e
  if ! [[ "$NODE_EXIT" -ne 0 && "$PYTHON_EXIT" -ne 0 && "$SUPPLY_EXIT" -ne 0 && "$ARCHIVE_EXIT" -ne 0 && \
    "$ROOTFS_EXIT" -ne 0 && "$INPUT_EXIT" -ne 0 && "$PROJECTION_EXIT" -ne 0 && "$NODE_MODULES_EXIT" -ne 0 && \
    "$ROUTE_EXIT" -eq 0 && "$STDOUT_EXIT" -ne 0 && \
    -z "$ROUTE_PROBE" && "$IDENTITY_EXIT" -eq 0 && "$SIBLING_EXIT" -eq 0 && "$FINAL_EXIT" -eq 0 ]]; then
    printf 'linux-probe-exits node=%s python=%s supply=%s archive=%s rootfs=%s input=%s projection=%s node_modules=%s route=%s stdout=%s identity=%s sibling=%s final=%s\n' \
      "$NODE_EXIT" "$PYTHON_EXIT" "$SUPPLY_EXIT" "$ARCHIVE_EXIT" "$ROOTFS_EXIT" "$INPUT_EXIT" \
      "$PROJECTION_EXIT" "$NODE_MODULES_EXIT" "$ROUTE_EXIT" "$STDOUT_EXIT" "$IDENTITY_EXIT" \
      "$SIBLING_EXIT" "$FINAL_EXIT" >&2
    fail "Linux isolation probes did not fail closed"
  fi
  if ! [[ "$NODE_PROBE" == *ENETUNREACH* && "$PYTHON_PROBE" == *"Network is unreachable"* && \
    "$SUPPLY_PROBE" == *"Read-only file system"* && "$ARCHIVE_PROBE" == *"Read-only file system"* && \
    "$ROOTFS_PROBE" == *"Read-only file system"* && "$INPUT_PROBE" == *"Read-only file system"* && \
    "$PROJECTION_PROBE" == *"Read-only file system"* && "$NODE_MODULES_PROBE" == *"Read-only file system"* && \
    "$STDOUT_PROBE" == *"No such file or directory"* ]]; then
    printf 'linux-probe-output node=%q python=%q supply=%q archive=%q rootfs=%q input=%q projection=%q node_modules=%q stdout=%q\n' \
      "$NODE_PROBE" "$PYTHON_PROBE" "$SUPPLY_PROBE" "$ARCHIVE_PROBE" "$ROOTFS_PROBE" \
      "$INPUT_PROBE" "$PROJECTION_PROBE" "$NODE_MODULES_PROBE" "$STDOUT_PROBE" >&2
    fail "Linux isolation probe errors drifted"
  fi
  printf '%s\n' "$identity" | /usr/bin/grep -q '^Uid:[[:space:]]*65534[[:space:]]*65534[[:space:]]*65534[[:space:]]*65534$'
  printf '%s\n' "$identity" | /usr/bin/grep -q '^Gid:[[:space:]]*65534[[:space:]]*65534[[:space:]]*65534[[:space:]]*65534$'
  printf '%s\n' "$identity" | /usr/bin/grep -q '^Groups:[[:space:]]*$'
  for field in CapInh CapPrm CapEff CapBnd CapAmb; do
    printf '%s\n' "$identity" | /usr/bin/grep -q "^$field:[[:space:]]*0000000000000000$"
  done
  printf '%s\n' "$identity" | /usr/bin/grep -q '^NoNewPrivs:[[:space:]]*1$'
  run_one linux-amd64 "$run" /work/input /work/ephemeral \
    "/projection/$PROJECTION_ARCHIVE_NAME" /projection/projection.json "$tree" \
    "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
  verify_authority_digests "$(wrapper_path)" /work/input "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
}

inner_linux_mount() {
  [[ "$#" -eq 12 ]] || fail "inner-linux-mount requires run tree rootfs work supply projection wrapper authority and digests"
  local run="$2" tree="$3" rootfs="$4" work="$5" supply="$6" projection="$7" wrapper_authority="$8"
  local wrapper_sha="$9" runner_sha="${10}" helper_sha="${11}" inspector_sha="${12}"
  for mountpoint in "$rootfs" "$rootfs/input" "$rootfs/projection" "$rootfs/work" \
    "$rootfs/authority" "$rootfs/tmp" "$rootfs/dev" "$rootfs/proc"; do
    [[ -d "$mountpoint" && ! -L "$mountpoint" ]] || fail "Linux mountpoint must be a regular directory: $mountpoint"
  done
  for workpoint in "$work" "$work/input" "$work/ephemeral" "$work/input/node_modules"; do
    [[ -d "$workpoint" && ! -L "$workpoint" ]] || fail "Linux workpoint must be a regular directory: $workpoint"
  done
  [[ -f "$rootfs/authority/replay-wrapper.sh" && ! -L "$rootfs/authority/replay-wrapper.sh" ]] ||
    fail "Linux rootfs authority wrapper mountpoint must be one regular file"
  require_canonical_regular_file linux-wrapper-authority "$wrapper_authority"
  [[ "sha256:$(sha256_file "$wrapper_authority")" == "$wrapper_sha" ]] || fail "Linux wrapper authority snapshot digest drifted"
  /usr/bin/touch "$rootfs/authority/replay-wrapper.sh"
  /bin/mount --make-rprivate /
  /bin/mount --bind "$rootfs" "$rootfs"
  /bin/mount --bind "$wrapper_authority" "$rootfs/authority/replay-wrapper.sh"
  /bin/mount -o remount,bind,ro "$rootfs/authority/replay-wrapper.sh"
  /bin/mount -o remount,bind,ro "$rootfs"
  /bin/mount --bind "$supply" "$rootfs/input"
  /bin/mount -o remount,bind,ro "$rootfs/input"
  /bin/mount --bind "$projection" "$rootfs/projection"
  /bin/mount -o remount,bind,ro "$rootfs/projection"
  /bin/mount --bind "$work" "$rootfs/work"
  /bin/mount --bind "$supply/npm-linux-glibc/node_modules" "$rootfs/work/input/node_modules"
  /bin/mount -o remount,bind,ro "$rootfs/work/input/node_modules"
  /bin/mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs "$rootfs/tmp"
  /bin/mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs "$rootfs/work/ephemeral"
  /bin/mount -t tmpfs -o mode=0755,nosuid tmpfs "$rootfs/dev"
  /bin/mknod -m 0666 "$rootfs/dev/null" c 1 3
  /bin/mknod -m 0666 "$rootfs/dev/zero" c 1 5
  /bin/mknod -m 0666 "$rootfs/dev/random" c 1 8
  /bin/mknod -m 0666 "$rootfs/dev/urandom" c 1 9
  /bin/mkdir -m 1777 "$rootfs/dev/shm"
  /bin/mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs "$rootfs/dev/shm"
  /bin/mount -t proc -o hidepid=2 proc "$rootfs/proc"
  [[ "sha256:$(sha256_file "$rootfs/authority/replay-wrapper.sh")" == "$wrapper_sha" ]] ||
    fail "Linux bound wrapper digest drifted before child"
  /usr/sbin/chroot "$rootfs" /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C LANG=C TZ=UTC \
    /bin/bash /authority/replay-wrapper.sh inner-linux-run \
    "$run" "$tree" "/work/sibling-must-be-absent" "$wrapper_sha" "$runner_sha" "$helper_sha" "$inspector_sha"
}

supervise_linux_run() {
  local capture="$1"; shift
  [[ ! -e "$capture" && ! -L "$capture" ]] || fail "Linux capture path must initially be absent"
  [[ ! -e "$capture.stderr" && ! -L "$capture.stderr" ]] || fail "Linux stderr path must initially be absent"
  "$TRUSTED_PYTHON" - "$capture" "$RUN_TIMEOUT_SECONDS" "$MAX_CAPTURE_BYTES" "$@" <<'PY'
import os
import selectors
import signal
import subprocess
import sys
import time

capture, timeout, max_bytes, *command = sys.argv[1:]
deadline = time.monotonic() + int(timeout)
process = subprocess.Popen(
    command,
    stdin=subprocess.DEVNULL,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    close_fds=True,
    start_new_session=True,
)
assert process.stdout is not None and process.stderr is not None
selector = selectors.DefaultSelector()
selector.register(process.stdout, selectors.EVENT_READ, "stdout")
selector.register(process.stderr, selectors.EVENT_READ, "stderr")
streams = {"stdout": bytearray(), "stderr": bytearray()}
while True:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
        raise SystemExit("isolated Linux run exceeded outer timeout")
    for key, _ in selector.select(min(remaining, 1.0)):
        chunk = os.read(key.fd, 65536)
        if chunk:
            streams[key.data].extend(chunk)
            if len(streams[key.data]) > int(max_bytes):
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise SystemExit(f"isolated Linux {key.data} exceeded v1 bound")
        else:
            selector.unregister(key.fileobj)
    if process.poll() is not None and not selector.get_map():
        break
status = process.wait()
with open(capture, "xb") as destination:
    destination.write(streams["stdout"])
with open(capture + ".stderr", "xb") as destination:
    destination.write(streams["stderr"])
if status != 0:
    sys.stderr.buffer.write(streams["stderr"])
    raise SystemExit(f"isolated Linux run failed with status {status}")
PY
  [[ "$(file_size "$capture")" -le "$MAX_CAPTURE_BYTES" ]] || fail "Linux isolated stdout exceeded v1 bound"
  [[ "$(file_size "$capture.stderr")" -le "$MAX_CAPTURE_BYTES" ]] || fail "Linux isolated stderr exceeded v1 bound"
}

run_linux() {
  [[ "$#" -eq 6 ]] || fail "run-linux requires <archive> <tree-sha> <task-root> <rootfs-tar> <supply-root>"
  local archive="$2" tree="$3" task="$4" rootfs_tar="$5" supply="$6"
  local wrapper metadata output rootfs_inspection wrapper_sha run lower run_root work capture
  local snapshot_archive snapshot_metadata snapshot_inspection snapshot_rootfs authority_wrapper
  wrapper="$(wrapper_path)"
  metadata="$(dirname "$archive")/projection.json"
  require_canonical_regular_file projection-archive "$archive"
  require_canonical_regular_file projection-metadata "$metadata"
  require_canonical_regular_file rootfs-tar "$rootfs_tar"
  require_canonical_regular_file wrapper "$wrapper"
  require_canonical_directory supply "$supply"
  require_fresh_canonical_leaf linux-task "$task"
  [[ "$tree" =~ ^[0-9a-f]{40}$ ]] || fail "projection tree SHA is invalid"
  for path in "$archive" "$metadata" "$rootfs_tar" "$wrapper" "$supply" "$task"; do require_profile_safe_path path "$path"; done
  [[ "$(sha256_file "$rootfs_tar")" == "$ROOTFS_TAR_SHA256" ]] || fail "Ubuntu export tar digest drifted"
  [[ "$(file_size "$rootfs_tar")" == "$ROOTFS_TAR_SIZE_BYTES" ]] || fail "Ubuntu export tar size drifted"
  tool_paths linux-amd64 "$supply"
  validate_supply_executables linux-amd64 "$supply"
  compute_authority_digests "$wrapper"
  /bin/mkdir -m 0700 "$task"
  output="$task/output"
  /bin/mkdir -m 0700 "$output" "$task/captures"
  /bin/mkdir -m 0755 "$task/projection" "$task/authority"
  snapshot_archive="$task/projection/$PROJECTION_ARCHIVE_NAME"
  snapshot_metadata="$task/projection/projection.json"
  snapshot_inspection="$task/projection/inspection.json"
  snapshot_rootfs="$task/rootfs.tar"
  authority_wrapper="$task/authority/replay-wrapper.sh"
  snapshot_file "$archive" "$snapshot_archive" projection-archive
  snapshot_file "$metadata" "$snapshot_metadata" projection-metadata
  snapshot_file "$rootfs_tar" "$snapshot_rootfs" rootfs-tar
  /bin/cp "$wrapper" "$authority_wrapper"
  /bin/chmod 0444 "$authority_wrapper"
  require_canonical_regular_file linux-authority-wrapper "$authority_wrapper"
  [[ "sha256:$(sha256_file "$authority_wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "Linux authority wrapper snapshot drifted"
  inspect_archive core-projection "$snapshot_archive" "$snapshot_inspection"
  validate_projection_snapshot "$snapshot_archive" "$snapshot_metadata" "$tree" "$snapshot_inspection"
  /bin/chmod 0666 "$snapshot_archive"
  [[ "$(/usr/bin/stat -c %a "$snapshot_archive")" == "666" ]] ||
    fail "Linux projection archive probe mode drifted"
  validate_projection_snapshot "$snapshot_archive" "$snapshot_metadata" "$tree" "$snapshot_inspection"
  rootfs_inspection="$task/rootfs-inspection.json"
  inspect_archive rootfs "$snapshot_rootfs" "$rootfs_inspection"
  [[ "$(sha256_file "$snapshot_rootfs")" == "$ROOTFS_TAR_SHA256" ]] || fail "Ubuntu snapshot export tar digest drifted"
  [[ "$(file_size "$snapshot_rootfs")" == "$ROOTFS_TAR_SIZE_BYTES" ]] || fail "Ubuntu snapshot export tar size drifted"
  for run in A B; do
    lower="$(printf '%s' "$run" | /usr/bin/tr '[:upper:]' '[:lower:]')"
    run_root="$task/run-$lower"
    [[ ! -e "$run_root" && ! -L "$run_root" && ! -e "$task/run-$([[ "$run" == A ]] && printf b || printf a)" && \
      ! -L "$task/run-$([[ "$run" == A ]] && printf b || printf a)" ]] ||
      fail "Linux replay roots are not independent and initially absent"
    /bin/mkdir -m 0700 "$run_root"
    /bin/mkdir "$run_root/rootfs" "$run_root/work"
    /usr/bin/tar -xf "$snapshot_rootfs" -C "$run_root/rootfs"
    for mountpoint in input projection work authority; do
      [[ ! -e "$run_root/rootfs/$mountpoint" && ! -L "$run_root/rootfs/$mountpoint" ]] || fail "rootfs mountpoint unexpectedly exists"
      /bin/mkdir "$run_root/rootfs/$mountpoint"
    done
    /bin/touch "$run_root/rootfs/authority/replay-wrapper.sh"
    work="$run_root/work"
    /bin/mkdir "$work/input" "$work/ephemeral"
    /usr/bin/tar -xf "$snapshot_archive" -C "$work/input"
    /bin/mkdir "$work/input/node_modules"
    /bin/chown -R 0:0 "$work"
    /bin/chmod -R a-w "$work/input"
    for output_file in "${GENERATOR_OUTPUT_FILES[@]}"; do
      [[ -f "$work/input/$output_file" && ! -L "$work/input/$output_file" ]] ||
        fail "Linux exact generator output must be an existing regular non-symlink file: $output_file"
      /bin/chown 65534:65534 "$work/input/$output_file"
      /bin/chmod u+rw "$work/input/$output_file"
    done
    capture="$task/captures/linux-$lower.frame"
    [[ ! -e "$capture" && ! -L "$capture" ]] || fail "Linux capture path must initially be absent"
    supervise_linux_run "$capture" \
      /usr/bin/unshare --net --mount --pid --fork --kill-child=SIGKILL \
      /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C LANG=C TZ=UTC \
      /bin/bash "$wrapper" inner-linux-mount "$run" "$tree" "$run_root/rootfs" "$work" \
      "$supply" "$task/projection" "$authority_wrapper" "$AUTHORITY_WRAPPER_SHA256" \
      "$AUTHORITY_RUNNER_SHA256" "$AUTHORITY_PATH_HELPER_SHA256" "$AUTHORITY_ARCHIVE_INSPECTOR_SHA256"
    parse_run_capture "$capture" "$output/linux-$lower.json" linux-amd64 "$run"
    /bin/rm -rf "$run_root"
    [[ ! -e "$run_root" && ! -L "$run_root" ]] || fail "Linux replay root was not destroyed after trusted capture"
  done
  [[ "sha256:$(sha256_file "$wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "wrapper bytes changed across Linux replay"
  [[ "sha256:$(sha256_file "$authority_wrapper")" == "$AUTHORITY_WRAPPER_SHA256" ]] || fail "Linux authority wrapper changed across replay"
  [[ "sha256:$(sha256_file "$(authority_runner_path)")" == "$AUTHORITY_RUNNER_SHA256" ]] || fail "runner bytes changed across Linux replay"
  [[ "sha256:$(sha256_file "$(authority_path_helper_path)")" == "$AUTHORITY_PATH_HELPER_SHA256" ]] || fail "path helper bytes changed across Linux replay"
  [[ "sha256:$(sha256_file "$(authority_archive_inspector_path)")" == "$AUTHORITY_ARCHIVE_INSPECTOR_SHA256" ]] || fail "archive inspector bytes changed across Linux replay"
  write_isolation_receipt linux-amd64 "$output" "$task/captures/linux-a.frame" \
    "$task/captures/linux-b.frame" "$wrapper" "$snapshot_archive" "$rootfs_inspection"
}

case "${1:-}" in
  build-projection) build_projection "$@" ;;
  run-darwin) run_darwin "$@" ;;
  inner-darwin-run) inner_darwin_run "$@" ;;
  run-linux) run_linux "$@" ;;
  inner-linux-mount) inner_linux_mount "$@" ;;
  inner-linux-run) inner_linux_run "$@" ;;
  *) fail "expected build-projection, run-darwin, or run-linux" ;;
esac
