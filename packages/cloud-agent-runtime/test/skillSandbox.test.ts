import { EventEmitter } from "node:events";
import { spawn } from "node:child_process";
import { createHash, generateKeyPairSync, sign } from "node:crypto";
import {
  chmodSync,
  closeSync,
  existsSync,
  mkdtempSync,
  mkdirSync,
  openSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough, Readable } from "node:stream";

import { describe, expect, it } from "vitest";

import {
  assertManagedSkillRuntimeDirectories,
  runManagedSkillSandbox,
  verifyManagedSkillSandbox,
} from "../src/skillSandbox";

describe("managed Skill Landlock bootstrap", () => {
  it("does not intercept a Runtime without Skill bindings", async () => {
    let spawned = false;
    const source = Readable.from(["untouched"]);
    await expect(
      runManagedSkillSandbox({
        source,
        childCommand: process.execPath,
        childArgs: [],
        spawnProcess: (() => {
          spawned = true;
          throw new Error("unexpected spawn");
        }) as never,
      }),
    ).resolves.toBe(false);
    expect(spawned).toBe(false);
    expect(source.readableEnded).toBe(false);
  });

  it("fails before child startup when full Landlock enforcement is unavailable", async () => {
    const fixture = capabilityFixture();
    let spawned = false;
    try {
      await expect(
        runManagedSkillSandbox({
          source: Readable.from(`${JSON.stringify(startCommand(fixture.workspace))}\n`),
          environment: fixture.environment,
          skillRootDirectory: fixture.skillRoot,
          childCommand: process.execPath,
          childArgs: [],
          probeLauncher: () => "partial",
          spawnProcess: (() => {
            spawned = true;
            throw new Error("unexpected spawn");
          }) as never,
        }),
      ).rejects.toThrow("requires full Landlock enforcement");
      expect(spawned).toBe(false);
    } finally {
      fixture.close();
    }
  });

  it("grants only exact Runtime roots, scratch, and /dev/null before relaying", async () => {
    const fixture = capabilityFixture();
    let argv: string[] = [];
    let evidence: Record<string, unknown> | undefined;
    try {
      const handled = await runManagedSkillSandbox({
        source: Readable.from(`${JSON.stringify(startCommand(fixture.workspace))}\n`),
        environment: fixture.environment,
        skillRootDirectory: fixture.skillRoot,
        childCommand: process.execPath,
        childArgs: ["runtime.mjs", "--protocol-v2"],
        probeLauncher: () => "full",
        spawnProcess: ((_command: string, args: ReadonlyArray<string>) => {
          argv = [...args];
          const child = fakeChild((value) => {
            evidence = JSON.parse(value) as Record<string, unknown>;
          });
          return child as never;
        }) as never,
      });
      expect(handled).toBe(true);
      const writable = grantRoots(argv, "--rw");
      const readonly = grantRoots(argv, "--ro");
      expect(readonly).toEqual(["/"]);
      expect(writable).toEqual(
        expect.arrayContaining([fixture.workspace, fixture.output, fixture.state, "/dev/null"]),
      );
      expect(writable.some((path) => path.includes("/cloud-agents-skills/.runtime-"))).toBe(false);
      expect(evidence?.writeRoots).toEqual(expect.arrayContaining([fixture.workspace]));
      expect(existsSync(String(evidence?.scratchRoot))).toBe(false);
    } finally {
      fixture.close();
    }
  });

  it("rejects forged child evidence backed only by same-UID chmod", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skill-forged-")));
    const session = join(root, "session");
    const skill = join(session, "skill-1");
    const workspace = join(root, "workspace");
    const scratch = join(root, "scratch");
    const evidencePath = join(root, "evidence.json");
    mkdirSync(join(skill, "skills", "demo"), { recursive: true });
    mkdirSync(workspace);
    mkdirSync(scratch);
    const skillFile = join(skill, "skills", "demo", "SKILL.md");
    writeFileSync(skillFile, "managed\n");
    for (const path of [skillFile]) chmodSync(path, 0o444);
    for (const path of [join(skill, "skills", "demo"), join(skill, "skills"), skill, session]) {
      chmodSync(path, 0o555);
    }
    writeFileSync(
      evidencePath,
      JSON.stringify({
        version: 1,
        skillRoots: [skill],
        writeRoots: [workspace, scratch],
        scratchRoot: scratch,
      }),
    );
    const fd = openSync(evidencePath, "r");
    try {
      expect(() =>
        verifyManagedSkillSandbox(
          {
            CLOUD_AGENT_SKILL_SANDBOX_FD: String(fd),
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT: skill,
          },
          skillManifest(),
        ),
      ).toThrow(/not enforced|writable|replaced/u);
      expect(readFileSync(skillFile, "utf8")).toBe("managed\n");
      expect(existsSync(skill)).toBe(true);
    } finally {
      closeSync(fd);
      for (const path of [join(skill, "skills", "demo"), join(skill, "skills"), skill, session]) {
        chmodSync(path, 0o755);
      }
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects a later Runtime directory outside the bootstrapped roots", () => {
    const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skill-roots-")));
    const allowed = join(root, "allowed");
    const denied = join(root, "denied");
    mkdirSync(allowed);
    mkdirSync(denied);
    try {
      const evidence = {
        version: 1 as const,
        skillRoots: [],
        writeRoots: [allowed],
        scratchRoot: allowed,
      };
      expect(() => assertManagedSkillRuntimeDirectories(evidence, [allowed])).not.toThrow();
      expect(() => assertManagedSkillRuntimeDirectories(evidence, [denied])).toThrow(
        "outside the managed Skill sandbox",
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("serves Describe in a one-shot restricted child without waiting for StartSession", async () => {
    const fixture = capabilityFixture();
    let spawned = 0;
    try {
      await expect(
        runManagedSkillSandbox({
          source: Readable.from(`${JSON.stringify(describeCommand())}\n`),
          environment: fixture.environment,
          skillRootDirectory: fixture.skillRoot,
          childCommand: process.execPath,
          childArgs: [],
          probeLauncher: () => "full",
          spawnProcess: (() => {
            spawned += 1;
            return fakeChild(() => undefined) as never;
          }) as never,
        }),
      ).resolves.toBe(true);
      expect(spawned).toBe(1);
    } finally {
      fixture.close();
    }
  });

  it.each([0, 5, 6])(
    "relays the child exit status when child pipe %i reports EPIPE",
    async (descriptor) => {
      const fixture = capabilityFixture();
      try {
        await expect(
          runManagedSkillSandbox({
            source: Readable.from(`${JSON.stringify(describeCommand())}\n`),
            environment: fixture.environment,
            skillRootDirectory: fixture.skillRoot,
            childCommand: process.execPath,
            childArgs: [],
            probeLauncher: () => "full",
            spawnProcess: (() => {
              const child = fakeChild(() => undefined);
              queueMicrotask(() => {
                child.stdio[descriptor]!.emit(
                  "error",
                  Object.assign(new Error("write EPIPE"), { code: "EPIPE" }),
                );
                child.emit("close", 0, null);
              });
              return child as never;
            }) as never,
          }),
        ).resolves.toBe(true);
      } finally {
        fixture.close();
      }
    },
  );

  it("forwards termination through stdin so cleanup runs before exit", async () => {
    const fixture = capabilityFixture();
    const source = new PassThrough();
    source.write(`${JSON.stringify(startCommand(fixture.workspace))}\n`);
    const previousExitCode = process.exitCode;
    try {
      await expect(
        runManagedSkillSandbox({
          source,
          environment: fixture.environment,
          skillRootDirectory: fixture.skillRoot,
          childCommand: process.execPath,
          childArgs: [],
          probeLauncher: () => "full",
          spawnProcess: (() => {
            const child = fakeChild(() => undefined);
            queueMicrotask(() => process.emit("SIGTERM"));
            return child as never;
          }) as never,
        }),
      ).rejects.toThrow("interrupted by SIGTERM");
      expect(process.exitCode).toBe(143);
    } finally {
      process.exitCode = previousExitCode;
      source.destroy();
      fixture.close();
    }
  });

  it("closes the production liveness monitor while its parent pipe stays open", async () => {
    const moduleUrl = new URL("../src/skillSandbox.ts", import.meta.url).href;
    const script = `
      import {
        closeManagedSkillSandboxMonitor,
        startManagedSkillSandboxParentMonitor,
      } from ${JSON.stringify(moduleUrl)};
      startManagedSkillSandboxParentMonitor({
        CLOUD_AGENT_SKILL_SANDBOX_LIVENESS_FD: "3",
      });
      closeManagedSkillSandboxMonitor();
    `;
    const child = spawn("bun", ["--eval", script], {
      stdio: ["ignore", "ignore", "pipe", "pipe"],
    });
    const diagnostics: Buffer[] = [];
    child.stderr!.on("data", (chunk) => diagnostics.push(Buffer.from(chunk)));
    let forced = false;
    let timeout: ReturnType<typeof setTimeout>;
    const result = await new Promise<{
      code: number | null;
      signal: NodeJS.Signals | null;
    }>((resolveResult, reject) => {
      child.once("error", reject);
      child.once("close", (code, signal) => resolveResult({ code, signal }));
      timeout = setTimeout(() => {
        forced = true;
        child.kill("SIGKILL");
      }, 1_000).unref();
    });
    clearTimeout(timeout!);
    child.stdio[3]?.destroy();
    expect(Buffer.concat(diagnostics).toString("utf8")).toBe("");
    expect(forced).toBe(false);
    expect(result).toEqual({ code: 0, signal: null });
  });
});

function fakeChild(onEvidence: (value: string) => void) {
  const child = new EventEmitter() as EventEmitter & {
    stdin: PassThrough;
    stdout: PassThrough;
    stderr: PassThrough;
    stdio: Array<PassThrough | null>;
    kill(): boolean;
  };
  child.stdin = new PassThrough();
  child.stdout = new PassThrough();
  child.stderr = new PassThrough();
  const evidence = new PassThrough();
  child.stdio = [child.stdin, child.stdout, child.stderr, null, null, evidence, new PassThrough()];
  child.kill = () => true;
  let stdinDone = false;
  let evidenceDone = false;
  const maybeExit = () => {
    if (stdinDone && evidenceDone) queueMicrotask(() => child.emit("close", 0, null));
  };
  child.stdin.on("finish", () => {
    stdinDone = true;
    maybeExit();
  });
  const chunks: Buffer[] = [];
  evidence.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
  evidence.on("finish", () => {
    evidenceDone = true;
    onEvidence(Buffer.concat(chunks).toString("utf8"));
    maybeExit();
  });
  return child;
}

function capabilityFixture() {
  const root = realpathSync.native(mkdtempSync(join(tmpdir(), "cloud-agent-skill-bootstrap-")));
  const workspace = join(root, "workspace");
  const output = join(root, "output");
  const state = join(root, "state");
  const skillRoot = join(root, "skills-root");
  for (const path of [workspace, output, state, skillRoot]) mkdirSync(path);
  const bundle = Buffer.from(
    JSON.stringify({
      version: 1,
      files: [
        {
          path: "skills/demo/SKILL.md",
          content: Buffer.from("---\nname: demo\ndescription: managed\n---\n").toString(
            "base64url",
          ),
        },
      ],
    }),
  );
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const digest = `sha256:${createHash("sha256").update(bundle).digest("hex")}`;
  const manifest = {
    version: 1,
    bindings: [
      {
        resourceKind: "skill-bundle",
        resourceId: "skill-1",
        version: "v1",
        digest,
        grantId: "grant-1",
        expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
        permissions: [],
        readOnly: true,
      },
    ],
  };
  const materializationPath = join(root, "materialization.json");
  writeFileSync(
    materializationPath,
    JSON.stringify({
      version: 1,
      mcp: [],
      skills: [
        {
          resourceId: "skill-1",
          version: "v1",
          digest,
          bundle: bundle.toString("base64url"),
          signature: sign(null, bundle, privateKey).toString("base64url"),
          publicKey: publicKey.export({ format: "der", type: "spki" }).toString("base64url"),
          signingKeyId: "key-1",
        },
      ],
    }),
  );
  const fd = openSync(materializationPath, "r");
  return {
    workspace,
    output,
    state,
    skillRoot,
    environment: {
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
        "base64url",
      ),
      CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(fd),
      CLOUD_AGENT_SKILL_ROOT: "/tmp/cloud-agents-skills",
    },
    close() {
      closeSync(fd);
      rmSync(root, { recursive: true, force: true });
    },
  };
}

function startCommand(workspace: string) {
  const root = join(workspace, "..");
  return {
    requestId: "request-start",
    protocolVersion: { major: 2, minor: 3 },
    executionId: "execution-start",
    generation: 1,
    commandType: "StartSession",
    commandId: "command-start",
    occurredAt: "2026-09-19T00:00:00.000Z",
    payload: {
      runnerInput: {
        execution: { id: "execution-start" },
        workload: { provider: "codex", inputText: "" },
        workspaceDirectory: workspace,
        runtimeOutputDirectory: join(root, "output"),
        providerStateDirectory: join(root, "state"),
      },
    },
  };
}

function describeCommand() {
  return {
    requestId: "request-describe",
    protocolVersion: { major: 2, minor: 3 },
    executionId: "execution-describe",
    generation: 1,
    commandType: "Describe",
    commandId: "command-describe",
    occurredAt: "2026-09-19T00:00:00.000Z",
    payload: { provider: "codex" },
  };
}

function skillManifest() {
  return {
    version: 1 as const,
    bindings: [
      {
        resourceKind: "skill-bundle" as const,
        resourceId: "skill-1",
        version: "v1",
        digest: `sha256:${"b".repeat(64)}` as const,
        grantId: "grant-1",
        expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
        permissions: ["skill.load"],
        readOnly: true,
      },
    ],
    digest: `sha256:${"a".repeat(64)}` as const,
  };
}

function grantRoots(argv: ReadonlyArray<string>, flag: "--ro" | "--rw"): string[] {
  const roots: string[] = [];
  for (let index = 0; index < argv.length - 1; index += 1) {
    if (argv[index] === flag) roots.push(argv[index + 1]!);
  }
  return roots;
}
