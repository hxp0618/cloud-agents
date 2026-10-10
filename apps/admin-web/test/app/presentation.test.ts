import { describe, expect, it } from "vitest";

import {
  executableFoundationNetworkPolicy,
  phaseTone,
  runtimeProfileCreatable,
} from "../../src/app/presentation";

describe("admin presentation rules", () => {
  it("keeps shared-untrusted network policy fail-closed", () => {
    const policy = {
      spec: {
        defaultEgress: "restricted" as const,
        allowedEgress: ["https://example.test"],
        previewEnabled: false,
        allowlistPolicyRef: undefined,
        dnsPolicyRef: undefined,
        proxyPolicyRef: undefined,
        ingressEnabled: false,
      },
    };
    expect(executableFoundationNetworkPolicy(policy as never, "shared-untrusted")).toBe(false);
  });

  it.each([
    ["docker", "deny", true],
    ["remote-worker", "restricted", true],
    ["docker", "public", false],
    ["ssh", "deny", false],
  ] as const)(
    "allows runtime profile creation for a %s target with %s egress: %s",
    (kind, egress, creatable) => {
      const target = { spec: { targetKind: kind } };
      const policy = {
        spec: {
          defaultEgress: egress,
          allowedEgress: egress === "restricted" ? ["example.test"] : [],
          previewEnabled: false,
          ingressEnabled: false,
        },
      };
      expect(runtimeProfileCreatable([target as never], [policy as never])).toBe(creatable);
    },
  );

  it("maps terminal phases to stable visual tones", () => {
    expect(phaseTone("succeeded")).toBe("success");
    expect(phaseTone("failed")).toBe("danger");
    expect(phaseTone("unrecognized")).toBe("neutral");
  });
});
