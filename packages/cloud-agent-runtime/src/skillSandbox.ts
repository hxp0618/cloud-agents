import { spawn, type ChildProcess, type SpawnOptions } from "node:child_process";
import {
  chmodSync,
  closeSync,
  existsSync,
  fstatSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readSync,
  readdirSync,
  realpathSync,
  renameSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { constants } from "node:fs";
import { Readable, type Writable } from "node:stream";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { Socket } from "node:net";

import { grantArgs, probe } from "@deepseek-ai/node-addon-landlock-run";
import {
  CLOUD_AGENT_MAX_COMMAND_BYTES,
  validateCloudAgentCommandEnvelope,
  type CloudAgentCommandEnvelope,
} from "@cloud-agents/cloud-agent-protocol";
import { CLOUD_AGENT_ENVIRONMENT } from "@cloud-agents/cloud-agent-provider-api";
import {
  managedSkillRootDirectory,
  readCapabilityManifest,
  type RuntimeCapabilityManifest,
} from "@cloud-agents/cloud-agent-provider-api/internal";

import { readCapabilityMaterialization } from "./capabilityBroker";
import { materializeManagedSkills, type ManagedSkillMounts } from "./skillMaterializer";

export const CLOUD_AGENT_LANDLOCK_LAUNCHER = "/usr/local/bin/cloud-agents-landlock-run";
const SANDBOX_EVIDENCE_ENV = "CLOUD_AGENT_SKILL_SANDBOX_FD";
const SANDBOX_EVIDENCE_CHILD_FD = 5;
const SANDBOX_LIVENESS_ENV = "CLOUD_AGENT_SKILL_SANDBOX_LIVENESS_FD";
const SANDBOX_LIVENESS_CHILD_FD = 6;
const MAX_EVIDENCE_BYTES = 64 * 1024;
const livenessSockets = new Set<Socket>();

export type ManagedSkillSandboxEvidence = Readonly<{
  version: 1;
  skillRoots: ReadonlyArray<string>;
  writeRoots: ReadonlyArray<string>;
  scratchRoot: string;
}>;

export type ManagedSkillSandboxOptions = Readonly<{
  source?: Readable;
  output?: Writable;
  diagnostics?: Writable;
  environment?: Readonly<Record<string, string | undefined>>;
  childCommand: string;
  childArgs: ReadonlyArray<string>;
  launcher?: string;
  probeLauncher?: typeof probe;
  spawnProcess?: typeof spawn;
  /** @internal Canonical Runtime-owned root injection for conformance tests. */
  skillRootDirectory?: string;
}>;

/** Launches the Runtime under Landlock when the manifest contains a Skill Bundle. */
export async function runManagedSkillSandbox(
  options: ManagedSkillSandboxOptions,
): Promise<boolean> {
  const environment = options.environment ?? process.env;
  if (environment[SANDBOX_EVIDENCE_ENV]) return false;
  const manifest = readCapabilityManifest(environment as NodeJS.ProcessEnv);
  if (!hasSkills(manifest)) return false;

  const materialization = readCapabilityMaterialization(environment, manifest);
  const mounts = materializeManagedSkills(
    manifest,
    materialization,
    options.skillRootDirectory ?? managedSkillRootDirectory(environment as NodeJS.ProcessEnv),
  );
  if (!mounts) throw new Error("Skill Bundle materialization is unavailable.");
  const scratchRoot = mkdtempSync("/tmp/cloud-agents-runtime-");
  try {
    const launcher = options.launcher ?? CLOUD_AGENT_LANDLOCK_LAUNCHER;
    if ((options.probeLauncher ?? probe)(launcher) !== "full") {
      throw new Error("Managed Skill isolation requires full Landlock enforcement.");
    }
    const source = options.source ?? process.stdin;
    const skillRoots = skillRootsFromMounts(mounts, manifest);
    const bootstrap = await readBootstrapCommands(source, async (frame) => {
      const writeRoots = prepareWriteRoots(undefined, scratchRoot, skillRoots);
      const evidence: ManagedSkillSandboxEvidence = Object.freeze({
        version: 1,
        skillRoots: Object.freeze(skillRoots),
        writeRoots: Object.freeze(writeRoots),
        scratchRoot,
      });
      await launchSandboxChild(options, Readable.from([]), frame, mounts, evidence, launcher);
    });
    if (!bootstrap.command && bootstrap.bytes.length === 0) return true;
    const writeRoots = prepareWriteRoots(bootstrap.command, scratchRoot, skillRoots);
    const evidence: ManagedSkillSandboxEvidence = Object.freeze({
      version: 1,
      skillRoots: Object.freeze(skillRoots),
      writeRoots: Object.freeze(writeRoots),
      scratchRoot,
    });
    await launchSandboxChild(options, source, bootstrap.bytes, mounts, evidence, launcher);
    return true;
  } finally {
    mounts.close();
    rmSync(scratchRoot, { recursive: true, force: true });
  }
}

/** Validates inherited evidence and actual kernel enforcement before broker/Provider startup. */
export function verifyManagedSkillSandbox(
  environment: Readonly<Record<string, string | undefined>>,
  manifest: RuntimeCapabilityManifest | null,
): ManagedSkillSandboxEvidence | null {
  if (!hasSkills(manifest)) return null;
  const rawFd = environment[SANDBOX_EVIDENCE_ENV]?.trim();
  if (!rawFd) throw new Error("Managed Skill Runtime is not isolated.");
  const fd = Number(rawFd);
  if (!Number.isSafeInteger(fd) || fd < 3 || fd > 1024) {
    throw new Error("Managed Skill sandbox evidence FD is invalid.");
  }
  const stat = fstatSync(fd);
  if (stat.size > MAX_EVIDENCE_BYTES) {
    throw new Error("Managed Skill sandbox evidence is too large.");
  }
  const parsed: unknown = JSON.parse(readBoundedFd(fd, MAX_EVIDENCE_BYTES).toString("utf8"));
  const evidence = parseEvidence(parsed);
  const expectedSkillRoots = skillBindings(manifest).map((binding) => {
    const suffix = binding.resourceId.replaceAll(/[^A-Za-z0-9]/gu, "_").toUpperCase();
    return canonicalEvidenceRoot(environment[`CLOUD_AGENT_SKILL_BUNDLE_${suffix}_ROOT`]);
  });
  if (
    evidence.skillRoots.length !== expectedSkillRoots.length ||
    evidence.skillRoots.some((root, index) => root !== expectedSkillRoots[index])
  ) {
    throw new Error("Managed Skill sandbox evidence does not match the manifest.");
  }
  for (const root of evidence.skillRoots) assertKernelReadOnly(root);
  for (const root of evidence.writeRoots) assertKernelWritable(root);
  startManagedSkillSandboxParentMonitor(environment);
  return evidence;
}

function readBoundedFd(fd: number, maximumBytes: number): Buffer {
  const chunks: Buffer[] = [];
  let total = 0;
  while (true) {
    const chunk = Buffer.allocUnsafe(4_096);
    const read = readSync(fd, chunk, 0, chunk.length, null);
    if (read === 0) return Buffer.concat(chunks, total);
    total += read;
    if (total > maximumBytes) {
      throw new Error("Managed Skill sandbox evidence is too large.");
    }
    chunks.push(chunk.subarray(0, read));
  }
}

export function assertManagedSkillRuntimeDirectories(
  evidence: ManagedSkillSandboxEvidence | null,
  directories: ReadonlyArray<string | undefined>,
): void {
  if (!evidence) return;
  for (const directory of directories) {
    if (!directory) continue;
    const canonical = canonicalDirectory(directory, false);
    if (!evidence.writeRoots.includes(canonical)) {
      throw new Error("Runtime directory is outside the managed Skill sandbox.");
    }
  }
}

/** Releases the child-side parent liveness watcher after Runtime shutdown. */
export function closeManagedSkillSandboxMonitor(): void {
  for (const socket of livenessSockets) socket.destroy();
  livenessSockets.clear();
}

function hasSkills(
  manifest: RuntimeCapabilityManifest | null,
): manifest is RuntimeCapabilityManifest {
  return skillBindings(manifest).length > 0;
}

function skillBindings(manifest: RuntimeCapabilityManifest | null) {
  return manifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? [];
}

function skillRootsFromMounts(
  mounts: ManagedSkillMounts,
  manifest: RuntimeCapabilityManifest,
): string[] {
  return skillBindings(manifest).map((binding) => {
    const suffix = binding.resourceId.replaceAll(/[^A-Za-z0-9]/gu, "_").toUpperCase();
    const root = mounts.environment[`CLOUD_AGENT_SKILL_BUNDLE_${suffix}_ROOT`];
    if (!root) throw new Error("Managed Skill mount is incomplete.");
    return canonicalDirectory(root, false);
  });
}

async function readBootstrapCommands(
  source: Readable,
  handleDescribe: (frame: Buffer) => Promise<void>,
): Promise<{ bytes: Buffer; command?: CloudAgentCommandEnvelope }> {
  let buffered = Buffer.alloc(0);
  for await (const value of source.iterator({ destroyOnReturn: false })) {
    buffered = Buffer.concat([buffered, Buffer.from(value)]);
    if (buffered.length > CLOUD_AGENT_MAX_COMMAND_BYTES * 8) {
      throw new Error("Managed Skill bootstrap input exceeds the bounded prefix.");
    }
    let offset = 0;
    while (true) {
      const newline = buffered.indexOf(0x0a, offset);
      if (newline < 0) break;
      const frame = buffered.subarray(offset, newline);
      offset = newline + 1;
      if (frame.length === 0) continue;
      if (frame.length > CLOUD_AGENT_MAX_COMMAND_BYTES) {
        throw new Error("Managed Skill bootstrap command exceeds the negotiated size limit.");
      }
      let parsed: unknown;
      try {
        parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(frame));
      } catch {
        throw new Error("Managed Skill bootstrap received an invalid command.");
      }
      const validation = validateCloudAgentCommandEnvelope(parsed);
      if (!validation.valid)
        throw new Error("Managed Skill bootstrap received an invalid command.");
      const command = parsed as CloudAgentCommandEnvelope;
      if (command.commandType === "StartSession" || command.commandType === "ResumeSession") {
        return { bytes: buffered, command };
      }
      if (command.commandType !== "Describe") {
        throw new Error("Managed Skill Runtime requires StartSession or ResumeSession first.");
      }
      await handleDescribe(buffered.subarray(0, offset));
      buffered = buffered.subarray(offset);
      offset = 0;
    }
  }
  return { bytes: buffered };
}

function prepareWriteRoots(
  command: CloudAgentCommandEnvelope | undefined,
  scratchRoot: string,
  skillRoots: ReadonlyArray<string>,
): string[] {
  const roots = [canonicalDirectory(scratchRoot, false)];
  if (command) {
    const runnerInput = record(command.payload.runnerInput);
    for (const name of ["workspaceDirectory", "runtimeOutputDirectory", "providerStateDirectory"]) {
      const value = runnerInput?.[name];
      if (value === undefined) continue;
      if (typeof value !== "string" || !isAbsolute(value)) {
        throw new Error("Managed Skill Runtime directory is invalid.");
      }
      roots.push(canonicalDirectory(value, true));
    }
    if (roots.length === 1) throw new Error("Managed Skill Runtime Workspace is required.");
  }
  const unique = [...new Set(roots)];
  for (const writeRoot of unique) {
    for (const skillRoot of skillRoots) {
      if (pathsOverlap(writeRoot, skillRoot)) {
        throw new Error("Managed Skill and writable Runtime roots overlap.");
      }
    }
  }
  return unique;
}

function canonicalDirectory(path: string, create: boolean): string {
  const absolute = resolve(path);
  if (absolute === sep || absolute !== path || !isAbsolute(path)) {
    throw new Error("Managed Skill sandbox root is invalid.");
  }
  if (create) mkdirSync(absolute, { recursive: true, mode: 0o700 });
  const info = lstatSync(absolute);
  const canonical = realpathSync.native(absolute);
  if (!info.isDirectory() || info.isSymbolicLink()) {
    throw new Error("Managed Skill sandbox root is untrusted.");
  }
  return canonical;
}

function pathsOverlap(left: string, right: string): boolean {
  const leftToRight = relative(left, right);
  const rightToLeft = relative(right, left);
  return (
    leftToRight === "" ||
    (!leftToRight.startsWith(`..${sep}`) && leftToRight !== "..") ||
    (!rightToLeft.startsWith(`..${sep}`) && rightToLeft !== "..")
  );
}

async function launchSandboxChild(
  options: ManagedSkillSandboxOptions,
  source: Readable,
  prefix: Buffer,
  mounts: ManagedSkillMounts,
  evidence: ManagedSkillSandboxEvidence,
  launcher: string,
): Promise<void> {
  const environment: NodeJS.ProcessEnv = {
    ...process.env,
    ...mounts.environment,
  };
  for (const [name, value] of Object.entries(options.environment ?? {})) {
    if (value === undefined) delete environment[name];
    else environment[name] = value;
  }
  const scratchHome = join(evidence.scratchRoot, "home");
  const scratchTmp = join(evidence.scratchRoot, "tmp");
  const scratchCache = join(evidence.scratchRoot, "cache");
  for (const directory of [scratchHome, scratchTmp, scratchCache]) {
    mkdirSync(directory, { recursive: true, mode: 0o700 });
  }
  environment.HOME = scratchHome;
  environment.TMPDIR = scratchTmp;
  environment.XDG_CACHE_HOME = scratchCache;
  environment.XDG_CONFIG_HOME = join(scratchHome, ".config");
  environment[SANDBOX_EVIDENCE_ENV] = String(SANDBOX_EVIDENCE_CHILD_FD);
  environment[SANDBOX_LIVENESS_ENV] = String(SANDBOX_LIVENESS_CHILD_FD);

  const credentialFd = inheritedFd(environment, CLOUD_AGENT_ENVIRONMENT.providerCredentialFd);
  const materializationFd = inheritedFd(
    environment,
    CLOUD_AGENT_ENVIRONMENT.capabilityMaterializationFd,
  );
  const stdio: SpawnOptions["stdio"] = [
    "pipe",
    "pipe",
    "pipe",
    credentialFd ?? "ignore",
    materializationFd ?? "ignore",
    "pipe",
    "pipe",
  ];
  if (credentialFd !== undefined) {
    environment[CLOUD_AGENT_ENVIRONMENT.providerCredentialFd] = "3";
  }
  if (materializationFd !== undefined) {
    environment[CLOUD_AGENT_ENVIRONMENT.capabilityMaterializationFd] = "4";
  }
  const argv = [
    ...grantArgs({
      readOnly: ["/"],
      readWrite: [...evidence.writeRoots, "/dev/null"],
    }),
    "--",
    options.childCommand,
    ...options.childArgs,
  ];
  const child = (options.spawnProcess ?? spawn)(launcher, argv, {
    detached: true,
    env: environment,
    stdio,
  });
  await relayChild(
    child,
    source,
    prefix,
    options.output ?? process.stdout,
    options.diagnostics ?? process.stderr,
    evidence,
  );
}

async function relayChild(
  child: ChildProcess,
  source: Readable,
  prefix: Buffer,
  output: Writable,
  diagnostics: Writable,
  evidence: ManagedSkillSandboxEvidence,
): Promise<void> {
  const childStdio = child.stdio as ReadonlyArray<Readable | Writable | null | undefined>;
  if (!child.stdin || !child.stdout || !child.stderr || !childStdio[5] || !childStdio[6]) {
    child.kill();
    throw new Error("Managed Skill sandbox child pipes are unavailable.");
  }
  child.stdout.pipe(output, { end: false });
  child.stderr.pipe(diagnostics, { end: false });
  const evidencePipe = childStdio[5] as Writable;
  const livenessPipe = childStdio[6] as Writable;
  evidencePipe.end(JSON.stringify(evidence));
  if (prefix.length > 0) child.stdin.write(prefix);
  if (source.readableEnded) child.stdin.end();
  else source.pipe(child.stdin);

  const forwarded = new Map<NodeJS.Signals, () => void>();
  let forwardedSignal: NodeJS.Signals | undefined;
  let forcedGroupStop: ReturnType<typeof setTimeout> | undefined;
  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    const handler = () => {
      forwardedSignal = signal;
      source.unpipe(child.stdin!);
      child.stdin?.end();
      process.exitCode = signal === "SIGTERM" ? 143 : 130;
      forcedGroupStop ??= setTimeout(() => killDetachedProcessGroup(child), 6_000);
    };
    forwarded.set(signal, handler);
    process.once(signal, handler);
  }
  try {
    const result = await new Promise<{
      code: number | null;
      signal: NodeJS.Signals | null;
    }>((resolveResult, reject) => {
      child.once("error", reject);
      child.once("close", (code, signal) => resolveResult({ code, signal }));
    });
    if (forwardedSignal) {
      throw new Error(`Managed Skill sandbox interrupted by ${forwardedSignal}.`);
    }
    if (result.signal) throw new Error(`Managed Skill sandbox child exited on ${result.signal}.`);
    if (result.code !== 0)
      throw new Error(`Managed Skill sandbox child exited with code ${result.code}.`);
  } finally {
    source.unpipe(child.stdin);
    livenessPipe.destroy();
    if (forcedGroupStop) clearTimeout(forcedGroupStop);
    killDetachedProcessGroup(child);
    for (const [signal, handler] of forwarded) process.off(signal, handler);
  }
}

function killDetachedProcessGroup(child: ChildProcess): void {
  if (!Number.isSafeInteger(child.pid) || !child.pid) return;
  try {
    process.kill(-child.pid, "SIGKILL");
  } catch {
    // The detached process group already exited.
  }
}

/** @internal Starts the inherited parent-death watcher used by the sandbox child. */
export function startManagedSkillSandboxParentMonitor(
  environment: Readonly<Record<string, string | undefined>>,
): void {
  const raw = environment[SANDBOX_LIVENESS_ENV]?.trim();
  const fd = Number(raw);
  if (!raw || !Number.isSafeInteger(fd) || fd < 3 || fd > 1024) {
    throw new Error("Managed Skill sandbox liveness FD is invalid.");
  }
  fstatSync(fd);
  const socket = new Socket({ fd, readable: true, writable: false });
  livenessSockets.add(socket);
  let stopping = false;
  const stop = () => {
    if (stopping) return;
    stopping = true;
    process.stdin.destroy();
    setTimeout(() => {
      try {
        process.kill(-process.pid, "SIGKILL");
      } catch {
        process.kill(process.pid, "SIGKILL");
      }
    }, 6_000);
  };
  socket.once("end", stop);
  socket.once("error", stop);
  socket.resume();
}

function inheritedFd(
  environment: Readonly<Record<string, string | undefined>>,
  name: string,
): number | undefined {
  const raw = environment[name]?.trim();
  if (!raw) return undefined;
  const fd = Number(raw);
  if (!Number.isSafeInteger(fd) || fd < 3 || fd > 1024) {
    throw new Error(`${name} is invalid.`);
  }
  fstatSync(fd);
  return fd;
}

function parseEvidence(value: unknown): ManagedSkillSandboxEvidence {
  const input = record(value);
  if (
    !input ||
    Object.keys(input).toSorted().join(",") !== "scratchRoot,skillRoots,version,writeRoots" ||
    input.version !== 1 ||
    !Array.isArray(input.skillRoots) ||
    !Array.isArray(input.writeRoots) ||
    typeof input.scratchRoot !== "string"
  ) {
    throw new Error("Managed Skill sandbox evidence is invalid.");
  }
  const skillRoots = input.skillRoots.map((root) => canonicalEvidenceRoot(root));
  const writeRoots = input.writeRoots.map((root) => canonicalEvidenceRoot(root));
  const scratchRoot = canonicalEvidenceRoot(input.scratchRoot);
  if (
    new Set(skillRoots).size !== skillRoots.length ||
    new Set(writeRoots).size !== writeRoots.length
  ) {
    throw new Error("Managed Skill sandbox roots are ambiguous.");
  }
  if (!writeRoots.includes(scratchRoot)) {
    throw new Error("Managed Skill sandbox scratch root is not writable.");
  }
  for (const writeRoot of writeRoots) {
    for (const skillRoot of skillRoots) {
      if (pathsOverlap(writeRoot, skillRoot)) {
        throw new Error("Managed Skill sandbox roots overlap.");
      }
    }
  }
  return Object.freeze({
    version: 1,
    skillRoots: Object.freeze(skillRoots),
    writeRoots: Object.freeze(writeRoots),
    scratchRoot,
  });
}

function canonicalEvidenceRoot(value: unknown): string {
  if (typeof value !== "string") throw new Error("Managed Skill sandbox root is invalid.");
  return canonicalDirectory(value, false);
}

function assertKernelReadOnly(root: string): void {
  assertCannotCreateInTree(root);
  assertCannotReplaceRoot(root);
}

function assertCannotCreateInTree(directory: string): void {
  const originalMode = lstatSync(directory).mode & 0o777;
  try {
    chmodSync(directory, originalMode | 0o700);
  } catch {
    return;
  }
  const probePath = join(directory, `.cloud-agent-readonly-probe-${process.pid}`);
  try {
    const fd = openSync(
      probePath,
      constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW,
      0o600,
    );
    closeSync(fd);
  } catch {
    chmodSync(directory, originalMode);
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) assertCannotCreateInTree(path);
      else if (entry.isFile()) assertCannotOpenForWrite(path);
    }
    return;
  }
  unlinkSync(probePath);
  chmodSync(directory, originalMode);
  throw new Error("Managed Skill sandbox is not enforced by the kernel.");
}

function assertCannotOpenForWrite(path: string): void {
  const originalMode = lstatSync(path).mode & 0o777;
  try {
    chmodSync(path, originalMode | 0o200);
  } catch {
    return;
  }
  try {
    const fd = openSync(path, constants.O_WRONLY | constants.O_NOFOLLOW);
    closeSync(fd);
  } catch {
    chmodSync(path, originalMode);
    return;
  }
  chmodSync(path, originalMode);
  throw new Error("Managed Skill file is writable inside the sandbox.");
}

function assertCannotReplaceRoot(root: string): void {
  const parent = dirname(root);
  const originalMode = lstatSync(parent).mode & 0o777;
  try {
    chmodSync(parent, originalMode | 0o700);
  } catch {
    return;
  }
  const probePath = `${root}.cloud-agent-rename-probe-${process.pid}`;
  try {
    renameSync(root, probePath);
  } catch {
    chmodSync(parent, originalMode);
    return;
  }
  renameSync(probePath, root);
  chmodSync(parent, originalMode);
  throw new Error("Managed Skill sandbox root can be replaced.");
}

function assertKernelWritable(root: string): void {
  const probePath = join(root, `.cloud-agent-write-probe-${process.pid}`);
  try {
    writeFileSync(probePath, "", { flag: "wx", mode: 0o600 });
  } finally {
    if (existsSync(probePath)) unlinkSync(probePath);
  }
}

function record(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}
