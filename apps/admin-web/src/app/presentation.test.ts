import { describe, expect, it } from "vitest";

import { executableFoundationNetworkPolicy, phaseTone } from "./presentation";

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

  it("maps terminal phases to stable visual tones", () => {
    expect(phaseTone("succeeded")).toBe("success");
    expect(phaseTone("failed")).toBe("danger");
    expect(phaseTone("unrecognized")).toBe("neutral");
  });
});
