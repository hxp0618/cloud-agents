import { spawnSync } from "node:child_process";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";

export const PLATFORM_GO_VERSION = "1.26.0";
export const PLATFORM_GO_TOOLCHAIN = "go1.26.6";

const GO_IMPORT_CHECKER = resolve(import.meta.dirname, "../go/importcheck/main.go");
const ISOLATED_GO_ENV = {
  GOTOOLCHAIN: "local",
  GOWORK: "off",
  GOFLAGS: "-mod=readonly",
} as const;

export const PLATFORM_GO_MODULES = [
  {
    directory: "sdk/go",
    module: "github.com/hxp0618/cloud-agents/sdk/go",
    forbiddenImports: ["github.com/hxp0618/cloud-agents/services/"],
  },
  {
    directory: "services/control-plane",
    module: "github.com/hxp0618/cloud-agents/services/control-plane",
    forbiddenImports: ["github.com/hxp0618/cloud-agents/services/worker"],
  },
  {
    directory: "services/worker",
    module: "github.com/hxp0618/cloud-agents/services/worker",
    forbiddenImports: ["github.com/hxp0618/cloud-agents/services/control-plane"],
  },
] as const;

export type ParsedGoModule = {
  readonly module: string;
  readonly goVersion: string;
  readonly toolchain: string;
  readonly hasReplace: boolean;
};

export type GoModuleEditDocument = {
  readonly Module?: { readonly Path?: string };
  readonly Go?: string;
  readonly Toolchain?: string;
  readonly Replace?: ReadonlyArray<unknown> | null;
};

export function parseGoModule(source: string): ParsedGoModule {
  const module = singleDirective(source, "module");
  const goVersion = singleDirective(source, "go");
  const toolchain = singleDirective(source, "toolchain");
  return {
    module,
    goVersion,
    toolchain,
    hasReplace: /^\s*replace(?:\s|\()/mu.test(source),
  };
}

export function validateGoModule(source: string, expectedModule: string): void {
  const parsed = parseGoModule(source);
  if (parsed.module !== expectedModule) {
    throw new Error(`Go module must be ${expectedModule}, found ${parsed.module}.`);
  }
  if (parsed.goVersion !== PLATFORM_GO_VERSION) {
    throw new Error(
      `${expectedModule} go directive must be ${PLATFORM_GO_VERSION}, found ${parsed.goVersion}.`,
    );
  }
  if (parsed.toolchain !== PLATFORM_GO_TOOLCHAIN) {
    throw new Error(
      `${expectedModule} toolchain must be ${PLATFORM_GO_TOOLCHAIN}, found ${parsed.toolchain}.`,
    );
  }
  if (parsed.hasReplace) {
    throw new Error(`${expectedModule} must not contain a replace directive.`);
  }
}

export function validateGoModuleEdit(
  document: GoModuleEditDocument,
  expectedModule: string,
  file: string,
): void {
  if (
    document.Module?.Path !== expectedModule ||
    document.Go !== PLATFORM_GO_VERSION ||
    document.Toolchain !== PLATFORM_GO_TOOLCHAIN
  ) {
    throw new Error(
      `${file} semantic identity mismatch: ${String(document.Module?.Path)} / ${String(document.Go)} / ${String(document.Toolchain)}.`,
    );
  }
  if ((document.Replace?.length ?? 0) !== 0) {
    throw new Error(`${file} must not contain replace directives.`);
  }
}

export function validateGoSourceImports(
  source: string,
  file: string,
  forbiddenPrefixes: ReadonlyArray<string>,
): void {
  const imports = goImportPaths(source, file);
  for (const imported of imports) {
    const forbidden = forbiddenPrefixes.find(
      (prefix) =>
        imported === prefix || imported.startsWith(prefix.endsWith("/") ? prefix : `${prefix}/`),
    );
    if (forbidden) {
      throw new Error(`${file} imports forbidden module boundary ${imported}.`);
    }
  }
}

export function validatePlatformGoModuleTree(root: string): void {
  for (const entry of PLATFORM_GO_MODULES) {
    const moduleRoot = resolve(root, entry.directory);
    validateGoModule(readFileSync(join(moduleRoot, "go.mod"), "utf8"), entry.module);
    for (const file of walkGoFiles(moduleRoot)) {
      validateGoSourceImports(
        readFileSync(file, "utf8"),
        relative(root, file).split(sep).join("/"),
        entry.forbiddenImports,
      );
    }
  }
}

function singleDirective(source: string, directive: string): string {
  const matches = [...source.matchAll(new RegExp(`^\\s*${directive}\\s+([^\\s]+)\\s*$`, "gmu"))];
  if (matches.length !== 1) {
    throw new Error(`go.mod must contain exactly one ${directive} directive.`);
  }
  return matches[0]![1]!;
}

function goImportPaths(source: string, file: string): ReadonlyArray<string> {
  const result = spawnSync("go", ["run", GO_IMPORT_CHECKER], {
    encoding: "utf8",
    env: { ...process.env, ...ISOLATED_GO_ENV },
    input: JSON.stringify({ file, source }),
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`Go import parser failed (${String(result.status)}):\n${result.stderr}`);
  }
  const parsed = JSON.parse(result.stdout) as { imports?: unknown };
  if (
    !Array.isArray(parsed.imports) ||
    !parsed.imports.every((value) => typeof value === "string")
  ) {
    throw new Error("Go import parser returned an invalid response.");
  }
  return parsed.imports;
}

function walkGoFiles(directory: string): ReadonlyArray<string> {
  const result: string[] = [];
  for (const name of readdirSync(directory)) {
    if (name === "vendor" || name.startsWith(".")) continue;
    const target = join(directory, name);
    const stat = statSync(target);
    if (stat.isDirectory()) result.push(...walkGoFiles(target));
    else if (name.endsWith(".go")) result.push(target);
  }
  return result;
}
