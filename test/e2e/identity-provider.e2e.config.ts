import { web } from "@e2e-dev/web";
import type { E2EConfig } from "e2e";

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") throw new Error(`${name} is required`);
  return value;
}

export default {
  projectId: "cloud-agents-identity-provider",
  tests: ["identity-provider.e2e.ts"],
  ...(["feishu", "github"].includes(process.env.CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_KIND ?? "")
    ? { timeout: 1_200_000, actionTimeout: 490_000 }
    : {}),
  output: ".tmp/identity-provider",
  workers: 1,
  cache: "off",
  trace: "off",
  targets: [
    {
      name: "identity-provider-chromium",
      engine: web({
        connect: { cdpEndpoint: async () => required("CLOUD_AGENTS_IDENTITY_E2E_CDP_ENDPOINT") },
        viewport: { width: 1440, height: 900 },
      }),
      app: {
        url: required("CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL"),
        environment: "test",
        identity: "cloud-agents-identity-provider",
      },
    },
  ],
} satisfies E2EConfig;
