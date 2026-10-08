import { resolve } from "node:path";
import { createHash } from "node:crypto";
import {
  assertImmutablePredecessor,
  assertSuccessorFiles,
  serializeSuccessor,
  successorDigest,
  successorSchema,
  writeSuccessorFiles,
  type ImmutablePredecessor,
} from "./lib/worker-localdev-profile-successor";
import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, readFileSync } from "node:fs";
import type { JsonRecord } from "./lib/platform-json-semantics";

const ROOT = resolve(import.meta.dirname, "..");
export const WORKER_LOCALDEV_SUCCESSOR_PATH =
  "contracts/worker/v1alpha1/localdev-profile-successor-v2.json";
export const WORKER_LOCALDEV_SUCCESSOR_SCHEMA_PATH =
  "contracts/worker/v1alpha1/localdev-profile-successor-v2.schema.json";

const LAUNCHER_PATHS = [
  "services/worker/localdev-launcher-profile/v1/authority-source.json",
  "services/worker/localdev-launcher-profile/v1/authority-source.schema.json",
  "services/worker/localdev-launcher-profile/v1/profile.json",
  "services/worker/localdev-launcher-profile/v1/profile.schema.json",
  "services/worker/localdev_launcher_profile_generated.go",
] as const;
const BRIDGE_PATHS = [
  "services/worker/localdev-bridge-profile/v1/authority-source.json",
  "services/worker/localdev-bridge-profile/v1/authority-source.schema.json",
  "services/worker/localdev-bridge-profile/v1/profile.json",
  "services/worker/localdev-bridge-profile/v1/profile.schema.json",
  "services/worker/localdev_bridge_profile_generated.go",
] as const;
const SUCCESSOR_GENERATED_PATHS = [
  "services/worker/localdev-launcher-profile/v2/authority-source.json",
  "services/worker/localdev-launcher-profile/v2/authority-source.schema.json",
  "services/worker/localdev-launcher-profile/v2/profile.json",
  "services/worker/localdev-launcher-profile/v2/profile.schema.json",
  "services/worker/localdev_launcher_profile_r2_generated.go",
  "services/worker/localdev-bridge-profile/v2/authority-source.json",
  "services/worker/localdev-bridge-profile/v2/authority-source.schema.json",
  "services/worker/localdev-bridge-profile/v2/profile.json",
  "services/worker/localdev-bridge-profile/v2/profile.schema.json",
  "services/worker/localdev_bridge_profile_r2_generated.go",
] as const;

const LAUNCHER_PREDECESSOR: ImmutablePredecessor = {
  commit: "f46ecdbc163e7c66d2ee87fed245e86f56a4e293",
  tree: "98db614adb31d036a9e97252b05a688d6d2f11fa",
  blobs: {
    [LAUNCHER_PATHS[0]]: "f147b95fa2933fed55a160beb3be5abb42d63e80",
    [LAUNCHER_PATHS[1]]: "7228642e2b75bd84552d4e1a7696e5c87af4a02b",
    [LAUNCHER_PATHS[2]]: "3a93cfe468645aee85438a6223c75af308cc4ccd",
    [LAUNCHER_PATHS[3]]: "daa8432bfd4394ad2eff74a08372a7010a0a764d",
    [LAUNCHER_PATHS[4]]: "a8e1846c82082a64679b7890a78cb9405b3f99e5",
  },
};
const BRIDGE_PREDECESSOR: ImmutablePredecessor = {
  commit: "b485eff2f3fbe91a90e6085666f380aeeb167369",
  tree: "920e089f79f66d55fd2ab0cf49884b0c70948374",
  blobs: {
    [BRIDGE_PATHS[0]]: "9a732bc619323daf37d4a86607319cdbc793e78f",
    [BRIDGE_PATHS[1]]: "fd35e1fe315ca65350607405b88fb917fa488f74",
    [BRIDGE_PATHS[2]]: "9cef59c0162fde878d9b5ff4fe07b067c3453532",
    [BRIDGE_PATHS[3]]: "b31115c68927be8238b17b6269fd65530d65a109",
    [BRIDGE_PATHS[4]]: "3d15801778e9eb4d21c3fc05641abea26349277d",
  },
};
const SHA256 = /^sha256:[0-9a-f]{64}$/u;

type DigestRecord = Readonly<{
  inputManifestDigest: string;
  sourceDigest: string;
  profileDigest: string;
}>;

function gitJson(root: string, predecessor: ImmutablePredecessor, path: string): JsonRecord {
  return JSON.parse(
    execFileSync("git", ["show", `${predecessor.commit}:${path}`], {
      cwd: root,
      encoding: "utf8",
    }),
  ) as JsonRecord;
}

function internalDigests(root: string, predecessor: ImmutablePredecessor, profilePath: string): DigestRecord {
  const profile = gitJson(root, predecessor, profilePath);
  const source = profile.sourceAuthority as JsonRecord;
  const value = {
    inputManifestDigest: profile.inputManifestDigest,
    sourceDigest: source.sourceDigest,
    profileDigest: profile.profileDigest,
  };
  for (const [key, digest] of Object.entries(value)) {
    if (typeof digest !== "string" || !SHA256.test(digest))
      throw new Error(`${profilePath} has invalid ${key}`);
  }
  return value as DigestRecord;
}

function fileSha256Records(root: string, predecessor: ImmutablePredecessor, paths: readonly string[]) {
  return paths.map((path) => {
    const bytes = execFileSync("git", ["cat-file", "blob", `${predecessor.commit}:${path}`], {
      cwd: root,
      encoding: "buffer",
    });
    return {
      path,
      gitBlob: predecessor.blobs[path],
      sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      sizeBytes: bytes.byteLength,
      mode: "100644",
    };
  });
}

function workingFileRecord(root: string, path: string): JsonRecord {
  const bytes = readFileSync(resolve(root, path));
  const stat = lstatSync(resolve(root, path));
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${path} must be a regular file`);
  return {
    path,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
    mode: (stat.mode & 0o111) === 0 ? "100644" : "100755",
  };
}

export function buildWorkerLocalDevProfileSuccessor(root: string = ROOT): JsonRecord {
  assertImmutablePredecessor(root, LAUNCHER_PREDECESSOR);
  assertImmutablePredecessor(root, BRIDGE_PREDECESSOR);
  const launcherDigests = internalDigests(
    root,
    LAUNCHER_PREDECESSOR,
    LAUNCHER_PATHS[2],
  );
  const bridgeDigests = internalDigests(root, BRIDGE_PREDECESSOR, BRIDGE_PATHS[2]);
  const body: JsonRecord = {
    formatVersion: "cloud-agents-worker-localdev-successor/v2",
    authorityId: "D-058-WORKER-LOCALDEV-SUCCESSOR-000001",
    revision: "D-058-WORKER-LOCALDEV-SUCCESSOR-000001.r1",
    status: "SUCCESSOR_PENDING_REBIND",
    notGateClosure: true,
    predecessorMutation: "forbidden",
    generator: {
      entrypoint: "scripts/generate-worker-localdev-profile-successor.ts",
      library: "scripts/lib/worker-localdev-profile-successor.ts",
      files: [
        workingFileRecord(root, "scripts/generate-worker-localdev-profile-successor.ts"),
        workingFileRecord(root, "scripts/lib/worker-localdev-profile-successor.ts"),
      ],
    },
    predecessors: {
      launcher: {
        authorityId: "D-056-WORKER-LOCALDEV-LAUNCHER-000001",
        revision: "D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1",
        profileId: "cloud-agents/worker-localdev-launcher/v1alpha1",
        commit: LAUNCHER_PREDECESSOR.commit,
        tree: LAUNCHER_PREDECESSOR.tree,
        files: fileSha256Records(root, LAUNCHER_PREDECESSOR, LAUNCHER_PATHS),
        internalDigests: launcherDigests,
      },
      bridge: {
        authorityId: "D-057-WORKER-LOCALDEV-BRIDGE-000001",
        revision: "D-057-WORKER-LOCALDEV-BRIDGE-000001.r1",
        profileId: "cloud-agents/worker-supervisor-operation-dispatch/launcher-localdev-v1alpha1",
        commit: BRIDGE_PREDECESSOR.commit,
        tree: BRIDGE_PREDECESSOR.tree,
        files: fileSha256Records(root, BRIDGE_PREDECESSOR, BRIDGE_PATHS),
        internalDigests: bridgeDigests,
      },
    },
    successor: {
      launcher: {
        authorityId: "D-056-WORKER-LOCALDEV-LAUNCHER-000001",
        revision: "D-056-WORKER-LOCALDEV-LAUNCHER-000001.r2",
        profileId: "cloud-agents/worker-localdev-launcher/v2alpha1",
        generatedPaths: SUCCESSOR_GENERATED_PATHS.slice(0, 5),
      },
      bridge: {
        authorityId: "D-057-WORKER-LOCALDEV-BRIDGE-000001",
        revision: "D-057-WORKER-LOCALDEV-BRIDGE-000001.r2",
        profileId: "cloud-agents/worker-supervisor-operation-dispatch/launcher-localdev-v2alpha1",
        parent: "D-056-WORKER-LOCALDEV-LAUNCHER-000001.r2",
        generatedPaths: SUCCESSOR_GENERATED_PATHS.slice(5),
      },
      consumerPaths: [
        "services/worker/cmd/cloud-agents-worker/main.go",
        "services/worker/cmd/cloud-agents-worker/main_test.go",
        "services/worker/supervisor/local_launcher.go",
        "services/worker/supervisor/local_launcher_test.go",
      ],
    },
    implementationBoundary: {
      runtimeRebind: "PENDING",
      providerSideEffects: "FORBIDDEN",
      deployment: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
      gateStatus: "ALL_GATES_OPEN",
    },
  };
  return { ...body, successorDigest: successorDigest("cloud-agents/worker-localdev-successor/v2", body) };
}

function files(root: string = ROOT): readonly [string, string][] {
  const value = buildWorkerLocalDevProfileSuccessor(root);
  const schema = successorSchema(
    value,
    "https://schemas.cloud-agents.dev/worker/localdev-successor/v2/schema.json",
    "D-058-WORKER-LOCALDEV-SUCCESSOR-000001",
  );
  return [
    [WORKER_LOCALDEV_SUCCESSOR_PATH, serializeSuccessor(value)],
    [WORKER_LOCALDEV_SUCCESSOR_SCHEMA_PATH, serializeSuccessor(schema)],
  ];
}

function assertSuccessorRuntimeOutputsAbsent(root: string): void {
  for (const path of SUCCESSOR_GENERATED_PATHS) {
    if (existsSync(resolve(root, path))) {
      throw new Error(`successor runtime output exists while rebind is pending: ${path}`);
    }
  }
}

export function checkWorkerLocalDevProfileSuccessor(root: string = ROOT): void {
  assertImmutablePredecessor(root, LAUNCHER_PREDECESSOR);
  assertImmutablePredecessor(root, BRIDGE_PREDECESSOR);
  const expected = files(root);
  assertSuccessorFiles(root, expected);
  assertSuccessorRuntimeOutputsAbsent(root);
}

export function writeWorkerLocalDevProfileSuccessor(root: string = ROOT): void {
  assertSuccessorRuntimeOutputsAbsent(root);
  writeSuccessorFiles(root, files(root));
}

const mode = process.argv[2] ?? "--check";
if (import.meta.main) {
  if (mode === "--check") {
    checkWorkerLocalDevProfileSuccessor(ROOT);
    process.stdout.write("worker-localdev-profile-successor: current\n");
  } else if (mode === "--write") {
    writeWorkerLocalDevProfileSuccessor(ROOT);
    process.stdout.write("worker-localdev-profile-successor: wrote authority\n");
  } else {
    throw new Error("Usage: bun scripts/generate-worker-localdev-profile-successor.ts --write|--check");
  }
}
