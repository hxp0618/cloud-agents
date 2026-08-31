import { describe, expect, it } from "vitest";

import {
  CLOUD_AGENT_ENVIRONMENT,
  readCloudAgentEnvironment,
  writeCloudAgentEnvironment,
} from "./environment";

describe("Cloud Agents environment", () => {
  it("reads and writes only the portable variable", () => {
    const name = CLOUD_AGENT_ENVIRONMENT.providerCredentialFd;
    const environment: Record<string, string> = {};
    writeCloudAgentEnvironment(environment, name, "3");
    expect(environment).toEqual({ [name]: "3" });
    expect(readCloudAgentEnvironment(environment, name)).toBe("3");
    expect(readCloudAgentEnvironment({ SYNARA_PROVIDER_CREDENTIAL_FD: "7" }, name)).toBeUndefined();
  });
});
