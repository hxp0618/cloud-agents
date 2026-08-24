import { spawnSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  lstatSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  requireEmptyReplayDirectory,
  frameReplayReport,
  parseReplayReportFrame,
  requireExactDirectoryEntries,
  requireFreshReplayPath,
} from "./lib/generator-replay-path-authority";
import {
  SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
} from "./lib/platform-successor-dag";

const root = resolve(import.meta.dirname, "..");
const runner = readFileSync(resolve(root, "scripts/replay-platform-generators.ts"), "utf8");
const wrapper = readFileSync(
  resolve(root, "scripts/replay-platform-generators-isolated.sh"),
  "utf8",
);
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const temporaryRoot of temporaryRoots.splice(0)) {
    rmSync(temporaryRoot, { recursive: true, force: true });
  }
});

describe("generator replay authority closure", () => {
  it("creates both native isolation boundaries in the versioned wrapper", () => {
    expect(wrapper).toContain("/usr/bin/sandbox-exec -f");
    expect(wrapper).toContain("/usr/bin/unshare --net --mount --pid --fork --kill-child=SIGKILL");
    expect(wrapper).toContain("SEPARATE_RUN_BOUNDARY_STDOUT_TRUSTED_PARENT_V1");
    expect(wrapper).toContain('"runAWriteRootDestroyedBeforeRunB": True');
    expect(wrapper).toContain('"sameProcessGroupEmptyAfterExit": "NOT_CLAIMED"');
    expect(wrapper).toContain('"processLifetimeClosure": "NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL"');
    expect(wrapper).toContain('"detachedDescendantsCrossRunReadDenied": False');
    expect(wrapper).not.toContain('"detachedDescendantsCrossRunReadDenied": True');
    expect(wrapper).toContain("detachedDescendantCrossBoundaryReadDenied");
    expect(wrapper).toContain('"sameBoundaryProbesAndReplay": True');
    expect(wrapper).toContain('[[ ! -e "$run_root" && ! -L "$run_root"');
    expect(wrapper).toContain('parse_run_capture "$capture"');
    expect(wrapper).toContain('/bin/rm -rf "$run_root"');
    expect(wrapper).toContain("close_fds=True");
    expect(wrapper).toContain("stdin=subprocess.DEVNULL");
    expect(wrapper).toContain("runner stdout frame");
    expect(wrapper).toContain('RUNNER_TIMEOUT_SECONDS="$((RUN_TIMEOUT_SECONDS - 60))"');
  });

  it("rejects non-canonical fresh projection and replay roots before creation", () => {
    const base = realpathSync(mkdtempSync(resolve(tmpdir(), "generator-wrapper-leaf-test-")));
    temporaryRoots.push(base);
    const realParent = resolve(base, "real-parent");
    const linkedParent = resolve(base, "linked-parent");
    mkdirSync(realParent);
    mkdirSync(resolve(realParent, "nested"));
    symlinkSync(realParent, linkedParent);
    const existing = resolve(base, "existing");
    writeFileSync(existing, "must remain\n");
    const dangling = resolve(base, "dangling");
    symlinkSync(resolve(base, "absent-target"), dangling);
    const cases = [
      { target: resolve(base, "missing-parent", "leaf"), message: "must be a regular" },
      { target: resolve(linkedParent, "leaf"), message: "must be a regular non-symlink" },
      { target: resolve(linkedParent, "nested", "leaf"), message: "must be canonical" },
      { target: `${realParent}/../lexical-leaf`, message: "must not contain lexical .." },
      { target: existing, message: "must initially be absent" },
      { target: dangling, message: "must initially be absent" },
    ];
    const executable = resolve(root, "scripts/replay-platform-generators-isolated.sh");
    for (const testCase of cases) {
      const result = spawnSync(executable, ["build-projection", root, testCase.target], {
        encoding: "utf8",
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain(testCase.message);
    }
    expect(readFileSync(existing, "utf8")).toBe("must remain\n");
    expect(() => lstatSync(resolve(realParent, "leaf"))).toThrow();
    expect(() => lstatSync(resolve(base, "lexical-leaf"))).toThrow();
    expect(() => lstatSync(resolve(base, "absent-target"))).toThrow();
    expect(
      wrapper.match(/require_fresh_canonical_leaf (projection-output|darwin-task|linux-task)/gu),
    ).toHaveLength(3);
    const projectionSection = wrapper.slice(
      wrapper.indexOf("build_projection()"),
      wrapper.indexOf("tool_paths()"),
    );
    const darwinSection = wrapper.slice(
      wrapper.indexOf("run_darwin()"),
      wrapper.indexOf("inner_linux_run()"),
    );
    const linuxSection = wrapper.slice(
      wrapper.indexOf("run_linux()"),
      wrapper.indexOf('case "${1:-}" in'),
    );
    expect(
      projectionSection.indexOf("require_fresh_canonical_leaf projection-output"),
    ).toBeLessThan(projectionSection.indexOf('/bin/mkdir -m 0700 "$output"'));
    expect(darwinSection.indexOf("require_fresh_canonical_leaf darwin-task")).toBeLessThan(
      darwinSection.indexOf('/bin/mkdir -m 0700 "$task"'),
    );
    expect(linuxSection.indexOf("require_fresh_canonical_leaf linux-task")).toBeLessThan(
      linuxSection.indexOf('/bin/mkdir -m 0700 "$task"'),
    );
  });

  it("builds the explicit acyclic core projection", () => {
    const expectedExclusions = [...SUCCESSOR_PROJECTION_EXCLUSIONS];
    const shellBlock = /readonly PROJECTION_EXCLUSIONS=\(\n([\s\S]*?)\n\)/u.exec(wrapper)?.[1];
    expect(shellBlock).toBeDefined();
    const shellExclusions = shellBlock!
      .split("\n")
      .map((line) => line.trim())
      .map((line) => JSON.parse(line) as string);
    expect(shellExclusions).toEqual(expectedExclusions);

    expect([...SUCCESSOR_PROJECTION_EXCLUSIONS]).toEqual(expectedExclusions);
    expect(wrapper).not.toContain("tools/generator-supply/v2/evidence/replay/**");
    expect(wrapper).not.toContain("g-contract-generator-supply-profile-v2-independent-review-*.md");
    expect(wrapper).toContain('trusted_git_index "$index" -c tar.umask=0022');
    expect(wrapper).toContain("/usr/bin/env -i PATH=/usr/bin:/bin HOME=/var/empty");
    expect(wrapper).toContain('readonly TRUSTED_GIT="/usr/bin/git"');
    expect(wrapper).toContain("reconstructedGitTreeSha");
    expect(runner).not.toContain("generate-platform-generator-supply-profile.ts");
    expect(runner).not.toContain("generate-platform-contract-lock.ts");
  });

  it("binds one exact 49-file output closure to both native write boundaries", () => {
    const expected = [
      "contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json",
      "contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json",
      "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json",
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json",
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
      "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-abort-terminal-writer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-ambiguous-resolution-writer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-commit-observation-writer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-recovery-admission-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-recovery-execution-admission-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-recovery-success-writer-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-retry-handoff-registry-v1.json",
      "contracts/generated/platform/v1alpha1/runner-ledger-return-failure-registry-v1.json",
      "contracts/generated/proto/cloud-agents-v1alpha1.binpb",
      "contracts/generated/proto/manifest.json",
      "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platform_adapter.pb.go",
      "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platformadapterv1alpha1connect/platform_adapter.connect.go",
      "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go",
      "sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go",
      "sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go",
      "sdk/go/gen/common/v1alpha1/identity_generated.go",
      "sdk/go/gen/common/v1alpha1/json_generated.go",
      "sdk/go/gen/openapi/v1alpha1/client_generated.go",
      "sdk/go/gen/platform/v1alpha1/json_generated.go",
      "sdk/go/generated-manifest.json",
      "sdk/go/json-generated-manifest.json",
      "sdk/go/proto-generated-manifest.json",
      "sdk/typescript/generated-manifest.json",
      "sdk/typescript/json-generated-manifest.json",
      "sdk/typescript/proto-generated-manifest.json",
      "sdk/typescript/src/gen/contracts/platform-adapter/v1alpha1/platform_adapter_pb.ts",
      "sdk/typescript/src/gen/contracts/worker/v1alpha1/kernel_pb.ts",
      "sdk/typescript/src/gen/contracts/worker/v1alpha1/worker_supervisor_pb.ts",
      "sdk/typescript/src/index.ts",
      "sdk/typescript/src/platform.ts",
      "sdk/typescript/src/proto.ts",
      "services/control-plane/internal/compatibility/registry_generated.go",
      "services/control-plane/internal/coordination/registry_generated.go",
      "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go",
      "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go",
      "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go",
      "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go",
      "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go",
    ];
    const excluded = [
      "contracts/generation.lock.json",
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json",
      "contracts/generated/platform/v1alpha1/identity-verifier-registry-v1.json",
      "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb",
      "tools/generator-supply/v2/evidence-manifest.json",
      "tools/generator-supply/v2/profile.json",
      "tools/contract-review-binding/v1/registry.json",
      "sdk/go/gen/common/v1alpha1/identity_generated_test.go",
      "sdk/go/gen/common/v1alpha1/json_generated_test.go",
      "sdk/go/gen/openapi/v1alpha1/client_generated_test.go",
      "sdk/go/gen/platform/v1alpha1/json_generated_test.go",
    ];
    const shellBlock = /readonly GENERATOR_OUTPUT_FILES=\(\n([\s\S]*?)\n\)/u.exec(wrapper)?.[1];
    expect(shellBlock).toBeDefined();
    const shellOutputs = shellBlock!.split("\n").map((line) => JSON.parse(line.trim()) as string);
    const runnerOutputs = [...SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS];
    expect(expected).toHaveLength(49);
    expect(new Set(expected).size).toBe(expected.length);
    expect(expected.toSorted()).toEqual(expected);
    expect(shellOutputs).toEqual(expected);
    expect(runnerOutputs).toEqual(expected);
    expect(runner).toContain(
      "const GENERATOR_OUTPUT_PATHS = SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS;",
    );
    for (const path of expected) {
      const metadata = lstatSync(resolve(root, path));
      expect(metadata.isFile()).toBe(true);
      expect(metadata.isSymbolicLink()).toBe(false);
    }
    for (const path of excluded) {
      expect(shellOutputs).not.toContain(path);
      expect(runnerOutputs).not.toContain(path);
    }
    expect(wrapper).not.toContain("GENERATOR_OUTPUT_TREES");
    expect(wrapper).not.toContain("for output_tree in");
    expect(runner).toContain("GENERATOR_OUTPUT_PATH_SET.has(file.path)");
    expect(runner).not.toContain('file.path.startsWith("contracts/generated/")');
    expect(runner).not.toContain('file.path.startsWith("sdk/go/gen/")');
    expect(runner).not.toContain('file.path.startsWith("sdk/typescript/src/gen/")');
    expect(wrapper.match(/for output_file in "\$\{GENERATOR_OUTPUT_FILES\[@\]\}"/gu)).toHaveLength(
      3,
    );
    expect(wrapper).toMatch(
      /for output_file in "\$\{GENERATOR_OUTPUT_FILES\[@\]\}"; do[\s\S]{0,300}allow file-write\* \(literal/u,
    );
    expect(wrapper).toMatch(
      /for output_file in "\$\{GENERATOR_OUTPUT_FILES\[@\]\}"; do[\s\S]{0,500}\/bin\/chown 65534:65534/u,
    );
  });

  it("does not trust caller-supplied network probe verdicts", () => {
    expect(runner).not.toContain("CLOUD_AGENTS_GENERATOR_NODE_NETWORK_PROBE");
    expect(runner).not.toContain("CLOUD_AGENTS_GENERATOR_PYTHON_NETWORK_PROBE");
    expect(runner).not.toContain('nodeNetworkProbe: "DENIED"');
    expect(runner).toContain(
      'isolationEvidenceAuthority: "VERSIONED_WRAPPER_SAME_BOUNDARY_RECEIPT"',
    );
  });

  it("binds exact fresh HOME, TMP, stdout frame, archive and input tree authority", () => {
    expect(runner).toContain("const replayHome = requireFreshReplayPath(");
    expect(runner).toContain("const replayTmp = requireEmptyReplayDirectory(");
    expect(runner).not.toContain("reportPath");
    expect(runner).toContain("process.stdout.write(frameReplayReport(report))");
    expect(runner).toContain('stdio: ["ignore", 2, 2]');
    expect(runner).toContain("projectionArchiveSha256: sourceArchiveSha256");
    expect(runner).toContain("inputTreeManifestSha256: inputTreeManifest.digest");
    expect(runner).toContain("projection archive regular-file content differs");
    expect(runner).toContain("projection archive does not reconstruct the exact Git tree");
    expect(runner).toContain(
      "const projectionInspection = inspectProjectionArchive(sourceArchive, archiveInspectorPython)",
    );
    expect(runner).not.toContain('spawnSync("/usr/bin/python3"');
    expect((runner.match(/^\s*runnerEnvironmentPolicy,/gmu) ?? []).length).toBe(1);
  });

  it("rejects stale or caller-divergent HOME and TMP paths", () => {
    const base = realpathSync(mkdtempSync(resolve(tmpdir(), "generator-replay-path-test-")));
    temporaryRoots.push(base);
    const authority = resolve(base, "authority");
    mkdirSync(authority);

    const home = resolve(authority, "home-a");
    expect(requireFreshReplayPath("HOME", home, authority, "home-a")).toBe(home);
    mkdirSync(home);
    expect(() => requireFreshReplayPath("HOME", home, authority, "home-a")).toThrow(
      /fresh absent path/u,
    );
    expect(() =>
      requireFreshReplayPath("HOME", resolve(base, "home-a"), authority, "home-a"),
    ).toThrow(/fresh absent path/u);

    const replayTemporary = resolve(authority, "tmp-a");
    mkdirSync(replayTemporary);
    expect(requireEmptyReplayDirectory("TMPDIR", replayTemporary, authority, "tmp-a")).toBe(
      replayTemporary,
    );
    writeFileSync(resolve(replayTemporary, "ambient"), "forbidden");
    expect(() =>
      requireEmptyReplayDirectory("TMPDIR", replayTemporary, authority, "tmp-a"),
    ).toThrow(/empty regular directory/u);
  });

  it("accepts one exact report frame and rejects garbage, truncation and size drift", () => {
    const report = { formatVersion: "test/v1", status: "PASS" };
    const frame = frameReplayReport(report);
    expect(parseReplayReportFrame(frame)).toEqual(report);
    expect(() => parseReplayReportFrame(`garbage\n${frame}`)).toThrow(/header/u);
    expect(() => parseReplayReportFrame(frame.slice(0, -1))).toThrow(/complete/u);
    expect(() => parseReplayReportFrame(frame.replace(/V1 [0-9]+/u, "V1 999999"))).toThrow(/size/u);
    expect(() => parseReplayReportFrame(`${frame}\n`)).toThrow(/size|trailing/u);
    expect(() => parseReplayReportFrame(`${frame}${frame}`)).toThrow(/size|trailing/u);
  });

  it("round-trips the bounded envelope and truthful receipt contract", () => {
    const report = {
      formatVersion: "cloud-agents-generator-replay-run/v1",
      platform: "darwin-arm64",
      replayRun: "A",
      projectionArchiveMembers: 3,
      detachedDescendantsCrossRunReadDenied: false,
      processLifetimeClosure: "NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL",
    };
    const runnerFrame = frameReplayReport(report);
    const envelope = {
      formatVersion: "cloud-agents-generator-isolated-run/v1",
      platform: "darwin-arm64",
      replayRun: "A",
      runnerFrame,
      probes: {
        nodeModules: {
          command: "unlink bound node_modules",
          exitCode: 1,
          stdout: "",
          stderr: "errno=1",
        },
      },
    };
    const parsedEnvelope = parseReplayReportFrame(frameReplayReport(envelope)) as typeof envelope;
    expect(parsedEnvelope.runnerFrame).toBe(runnerFrame);
    expect(parsedEnvelope.probes.nodeModules.exitCode).toBe(1);
    expect(Number.isSafeInteger(report.projectionArchiveMembers)).toBe(true);
    expect(report.projectionArchiveMembers).toBeGreaterThan(0);
    expect(report.detachedDescendantsCrossRunReadDenied).toBe(false);
  });

  it("treats a dangling authority symlink as occupied", () => {
    const base = realpathSync(mkdtempSync(resolve(tmpdir(), "generator-replay-dangling-test-")));
    temporaryRoots.push(base);
    const authority = resolve(base, "authority");
    mkdirSync(authority);
    const dangling = resolve(authority, "home-a");
    symlinkSync("missing", dangling);
    expect(() => requireFreshReplayPath("HOME", dangling, authority, "home-a")).toThrow(
      /fresh absent path/u,
    );
  });

  it("rejects every undeclared wheelhouse entry including directories and symlinks", () => {
    const base = realpathSync(mkdtempSync(resolve(tmpdir(), "generator-wheelhouse-test-")));
    temporaryRoots.push(base);
    writeFileSync(resolve(base, "one.whl"), "one");
    expect(requireExactDirectoryEntries("wheelhouse", base, ["one.whl"])).toEqual(["one.whl"]);
    mkdirSync(resolve(base, "cache"));
    expect(() => requireExactDirectoryEntries("wheelhouse", base, ["one.whl"])).toThrow(
      /exact directory entry closure/u,
    );
    rmSync(resolve(base, "cache"), { recursive: true });
    symlinkSync("one.whl", resolve(base, "alias.whl"));
    expect(() => requireExactDirectoryEntries("wheelhouse", base, ["one.whl"])).toThrow(
      /exact directory entry closure/u,
    );
  });

  it("keeps the trusted Linux runner root and drops only candidate children", () => {
    const mount = wrapper.indexOf('/bin/mount --bind "$rootfs"');
    const chroot = wrapper.indexOf('/usr/sbin/chroot "$rootfs"');
    expect(mount).toBeGreaterThan(0);
    expect(chroot).toBeGreaterThan(mount);
    expect(wrapper).toContain("candidate_exec()");
    expect(runner).toContain("/usr/bin/setpriv");
    expect(runner).toContain("assertNoCandidateProcesses");
    expect(wrapper).toContain("/usr/bin/chown 65534:65534");
    expect(wrapper).toContain(
      '/bin/mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs "$rootfs/work/ephemeral"',
    );
    expect(wrapper).toContain(
      "--bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs",
    );
    expect(wrapper).toContain("CapEff");
    expect(wrapper).toContain("NoNewPrivs");
  });

  it("rejects sandbox profile injection characters and denies trusted output reads", () => {
    expect(wrapper).toContain("require_profile_safe_path");
    expect(wrapper).toContain("contains sandbox profile metacharacters");
    expect(wrapper).toContain('(deny file-read* (subpath "%s"))');
    expect(wrapper).toContain("detached descendant cross-boundary probe");
    expect(wrapper).toContain("posix_spawn cross-boundary probe");
    expect(wrapper).toContain("replace the bound node_modules symlink");
    expect(wrapper).toContain("NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL");
    expect(wrapper).toContain("(deny mach-lookup)");
    expect(wrapper).toContain('(allow file-read-data (literal "/"))');
    for (const executable of ["/bin/mkdir", "/bin/pwd", "/bin/test", "/usr/bin/basename"]) {
      expect(wrapper).toContain(`(literal "${executable}")`);
    }
    expect(wrapper).not.toContain("background descendants are all destroyed");
  });
});
