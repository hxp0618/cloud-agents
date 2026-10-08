import { chmodSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { describe, expect, it } from "vitest";

const wrapper = resolve("test/e2e/test-foundation-controller-kubernetes-orbstack-vm.sh");

describe("OrbStack VM Kubernetes route wrapper", () => {
  it("requires an explicit VM, kubeconfig, context and new output directory", () => {
    const result = spawnSync("sh", [wrapper], { encoding: "utf8" });
    expect(result.status).toBe(2);
    expect(result.stderr).toContain(
      "--vm VM --kubeconfig FILE --context CONTEXT --output-dir NEW_DIR",
    );
  });

  it("fails closed on an existing exact route before starting the lifecycle", () => {
    const fixture = createFixture();
    const result = runWrapper(fixture, { FAKE_ROUTE_COUNT: "1" });
    expect(result.status).not.toBe(0);
    expect(readFileSync(fixture.calls, "utf8")).not.toContain("mise:");
    expect(result.stderr).toContain("already has an exact route");
  });

  it("rejects a context that is not the selected single-node VM", () => {
    const fixture = createFixture();
    const result = runWrapper(fixture, {
      FAKE_NODES_JSON: JSON.stringify({
        items: [
          {
            spec: { podCIDR: "10.42.0.0/24" },
            status: { addresses: [{ type: "InternalIP", address: "192.0.2.11" }] },
          },
        ],
      }),
    });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("does not match the explicit context node InternalIP");
    expect(readFileSync(fixture.calls, "utf8")).not.toContain("run --rm");
  });

  it("rejects a context with more than one node", () => {
    const fixture = createFixture();
    const node = {
      spec: { podCIDR: "10.42.0.0/24" },
      status: { addresses: [{ type: "InternalIP", address: "192.0.2.10" }] },
    };
    const result = runWrapper(fixture, {
      FAKE_NODES_JSON: JSON.stringify({ items: [node, node] }),
    });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("exactly one node");
    expect(readFileSync(fixture.calls, "utf8")).not.toContain("run --rm");
  });

  it("fails closed unless the API exposes one authoritative ServiceCIDR", () => {
    const fixture = createFixture();
    const result = runWrapper(fixture, { FAKE_SERVICE_CIDRS_JSON: '{"items":[]}' });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("one authoritative IPv4 ServiceCIDR");
    expect(readFileSync(fixture.calls, "utf8")).not.toContain("run --rm");
  });

  it("runs the existing lifecycle and restores only its exact routes", () => {
    const fixture = createFixture();
    const result = runWrapper(fixture);
    expect(result.status).toBe(0);
    expect(JSON.parse(readFileSync(join(fixture.output, "route-before.json"), "utf8"))).toEqual({
      podExactRouteCount: 0,
      serviceExactRouteCount: 0,
    });
    expect(JSON.parse(readFileSync(join(fixture.output, "route-after.json"), "utf8"))).toEqual({
      podExactRouteCount: 0,
      serviceExactRouteCount: 0,
    });
    expect(JSON.parse(readFileSync(join(fixture.output, "route-repaired.json"), "utf8"))).toEqual({
      podRouteViaVm: true,
      serviceRouteViaVm: true,
      podGatewayPing: true,
      serviceApiTcp: true,
    });
    expect(readFileSync(join(fixture.output, "harness.exit"), "utf8")).toBe("0\n");
    expect(readFileSync(join(fixture.output, "route-helper.exit"), "utf8")).toBe("0\n");
    const calls = readFileSync(fixture.calls, "utf8");
    expect(calls).toContain("mise:");
    for (const call of calls.split("\n").filter((line) => line.startsWith("kubectl:"))) {
      expect(call).toMatch(/^kubectl:--kubeconfig \S+ --context explicit /u);
    }
  });

  it("preserves the lifecycle exit code after restoring the routes", () => {
    const fixture = createFixture();
    const result = runWrapper(fixture, { FAKE_HARNESS_EXIT: "7" });
    expect(result.status).toBe(7);
    expect(readFileSync(join(fixture.output, "harness.exit"), "utf8")).toBe("7\n");
    expect(readFileSync(join(fixture.output, "route-helper.exit"), "utf8")).toBe("0\n");
    expect(JSON.parse(readFileSync(join(fixture.output, "route-after.json"), "utf8"))).toEqual({
      podExactRouteCount: 0,
      serviceExactRouteCount: 0,
    });
  });
});

type Fixture = Readonly<{
  root: string;
  bin: string;
  calls: string;
  kubeconfig: string;
  output: string;
}>;

function createFixture(): Fixture {
  const root = mkdtempSync(join(tmpdir(), "foundation-kubernetes-orbstack-route-"));
  const bin = join(root, "bin");
  const calls = join(root, "calls.log");
  const kubeconfig = join(root, "kubeconfig");
  const output = join(root, "output");
  mkdirSync(bin);
  writeFileSync(calls, "");
  writeFileSync(kubeconfig, "fixture\n", { mode: 0o600 });
  writeExecutable(
    join(bin, "kubectl"),
    `#!/bin/sh
set -eu
printf 'kubectl:%s\\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  *'config get-contexts -o name'*) printf '%s\\n' explicit ;;
  *'get --raw=/readyz'*) printf '%s\\n' ok ;;
  *'get node -o json'*) printf '%s' "$FAKE_NODES_JSON" ;;
  *'get service kubernetes -n default -o jsonpath='*) printf '%s' 10.43.0.1 ;;
  *'get servicecidrs.networking.k8s.io -o json'*) printf '%s' "$FAKE_SERVICE_CIDRS_JSON" ;;
  *'get ns -o name'*) : ;;
  *) printf 'unexpected kubectl call\\n' >&2; exit 64 ;;
esac
`,
  );
  writeExecutable(
    join(bin, "orb"),
    `#!/bin/sh
set -eu
printf 'orb:%s\\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  list) printf '%s\\n' 'fixture-vm running ubuntu noble arm64' ;;
  *'hostname -I'*) printf '%s\\n' '192.0.2.10' ;;
  *) printf 'unexpected orb call\\n' >&2; exit 64 ;;
esac
`,
  );
  writeExecutable(
    join(bin, "docker"),
    `#!/bin/sh
set -eu
printf 'docker:%s\\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  *'image inspect'*) exit 0 ;;
  *'run --rm'*) printf '%s\\n' "\${FAKE_ROUTE_COUNT:-0}" ;;
  *'run -d'*)
    evidence=''
    previous=''
    for value in "$@"; do
      if [ "$previous" = '-v' ]; then evidence=\${value%%:/evidence}; fi
      previous=$value
    done
    test -n "$evidence"
    printf '%s\\n' '{"podExactRouteCount":0,"serviceExactRouteCount":0}' > "$evidence/route-before.json"
    printf '%s\\n' '{"podRouteViaVm":true,"serviceRouteViaVm":true,"podGatewayPing":true,"serviceApiTcp":true}' > "$evidence/route-repaired.json"
    : > "$evidence/route-ready"
    printf '%s\\n' fixture-helper-id
    ;;
  *'ps '* ) : ;;
  *'wait '* )
    printf '%s\\n' '{"podExactRouteCount":0,"serviceExactRouteCount":0}' > "$FAKE_OUTPUT/route-after.json"
    printf '%s\\n' 0
    ;;
  *'logs '* ) : ;;
  *'rm '* ) : ;;
  *) printf 'unexpected docker call\\n' >&2; exit 64 ;;
esac
`,
  );
  writeExecutable(
    join(bin, "mise"),
    `#!/bin/sh
set -eu
printf 'mise:%s\\n' "$*" >> "$FAKE_CALLS"
mkdir -p "$FAKE_FOUNDATION_OUTPUT"
exit "\${FAKE_HARNESS_EXIT:-0}"
`,
  );
  return { root, bin, calls, kubeconfig, output };
}

function runWrapper(fixture: Fixture, extraEnv: NodeJS.ProcessEnv = {}) {
  return spawnSync(
    "sh",
    [
      wrapper,
      "--vm",
      "fixture-vm",
      "--kubeconfig",
      fixture.kubeconfig,
      "--context",
      "explicit",
      "--output-dir",
      fixture.output,
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        FAKE_NODES_JSON: JSON.stringify({
          items: [
            {
              spec: { podCIDR: "10.42.0.0/24" },
              status: { addresses: [{ type: "InternalIP", address: "192.0.2.10" }] },
            },
          ],
        }),
        FAKE_SERVICE_CIDRS_JSON: JSON.stringify({
          items: [{ spec: { cidrs: ["10.43.0.0/16"] } }],
        }),
        ...extraEnv,
        PATH: `${fixture.bin}:${process.env.PATH}`,
        FAKE_CALLS: fixture.calls,
        FAKE_OUTPUT: fixture.output,
        FAKE_FOUNDATION_OUTPUT: join(fixture.output, "foundation"),
      },
    },
  );
}

function writeExecutable(path: string, source: string): void {
  writeFileSync(path, source);
  chmodSync(path, 0o755);
}
