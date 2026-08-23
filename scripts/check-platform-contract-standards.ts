import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PYTHON_VERSION = "3.14.7";
const UV_VERSION = "0.12.5";
const BUN_VERSION = "1.3.14";
const root = resolve(import.meta.dirname, "..");
const project = resolve(root, "tools/contract-standards");
const wheelhouse = process.env.CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE;

function commandOutput(command: string, args: readonly string[]): string {
  const result = spawnSync(command, args, { cwd: root, encoding: "utf8" });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}):\n${result.stdout}${result.stderr}`,
    );
  }
  const stdout = result.stdout.trim();
  return stdout === "" ? result.stderr.trim() : stdout;
}

const pythonVersion = commandOutput("python3", ["--version"]).replace(/^Python\s+/u, "");
const uvVersion = commandOutput("uv", ["--version"]).match(/^uv\s+(\S+)/u)?.[1] ?? "";
const bunVersion = commandOutput("bun", ["--version"]);
if (pythonVersion !== PYTHON_VERSION || uvVersion !== UV_VERSION || bunVersion !== BUN_VERSION) {
  throw new Error(
    `Contract standards toolchain mismatch: expected=${JSON.stringify({ bun: BUN_VERSION, python: PYTHON_VERSION, uv: UV_VERSION })} actual=${JSON.stringify({ bun: bunVersion, python: pythonVersion, uv: uvVersion })}.`,
  );
}

commandOutput("uv", ["lock", "--project", project, "--check"]);
const requirements = commandOutput("uv", [
  "export",
  "--project",
  project,
  "--locked",
  "--format",
  "requirements-txt",
  "--no-dev",
  "--no-header",
  "--no-emit-project",
]);

function run(
  command: string,
  args: readonly string[],
  options?: { readonly input?: string },
): void {
  const result = spawnSync(command, args, {
    cwd: root,
    env: process.env,
    input: options?.input,
    stdio: options?.input === undefined ? "inherit" : ["pipe", "inherit", "inherit"],
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(
      `contract standards command failed (${String(result.status)}): ${command} ${args.join(" ")}`,
    );
  }
}

function requirementBlock(requirementsText: string, packageName: string): string {
  const lines = requirementsText.split("\n");
  const start = lines.findIndex((line) => line.startsWith(`${packageName}==`));
  if (start < 0) throw new Error(`missing exported requirement: ${packageName}`);
  let end = start + 1;
  while (end < lines.length && (lines[end] === "" || /^\s/u.test(lines[end] ?? ""))) end += 1;
  return `${lines.slice(start, end).join("\n")}\n`;
}

run("bun", ["scripts/check-platform-contracts.ts"]);

const temporaryRoot = mkdtempSync(join(tmpdir(), "cloud-agents-contract-standards-"));
try {
  const environment = join(temporaryRoot, "venv");
  run("uv", ["venv", "--python", PYTHON_VERSION, "--no-python-downloads", environment]);
  const python = join(environment, "bin", "python");
  if (wheelhouse !== undefined) {
    run(
      "uv",
      [
        "pip",
        "install",
        "--python",
        python,
        "--require-hashes",
        "--no-build",
        "--no-index",
        "--find-links",
        wheelhouse,
        "--requirements",
        "-",
      ],
      { input: requirementBlock(requirements, "jsonschema-rs") },
    );
  }
  run(
    "uv",
    ["pip", "sync", "--python", python, "--require-hashes", "--no-build", "--strict", "-"],
    { input: `${requirements}\n` },
  );
  run(python, ["tools/contract-standards/check_contract_standards.py", "--root", "."]);
  run(python, ["-m", "unittest", "discover", "-s", "tools/contract-standards", "-p", "test_*.py"]);
  process.stdout.write("platform-contract-standards: current non-Gate candidate\n");
} finally {
  rmSync(temporaryRoot, { force: true, recursive: true });
}
