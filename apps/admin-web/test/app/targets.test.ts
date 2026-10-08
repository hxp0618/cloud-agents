import { describe, expect, it } from "vitest";

import { deploymentTargetRegisterRequestFrom, targetRegistrationForm } from "../../src/app/targets";

describe("target registration form mapping", () => {
  it("trims registration fields without changing the selected target kind", () => {
    expect(
      deploymentTargetRegisterRequestFrom({
        ...targetRegistrationForm(),
        targetId: " ssh-overflow ",
        targetName: " SSH Overflow ",
        targetKind: "ssh",
        endpoint: " ssh://worker.example.test:22 ",
        credentialRef: " ssh-primary ",
      }),
    ).toEqual({
      targetId: "ssh-overflow",
      targetName: "SSH Overflow",
      targetKind: "ssh",
      endpoint: "ssh://worker.example.test:22",
      credentialRef: "ssh-primary",
    });
  });
});
