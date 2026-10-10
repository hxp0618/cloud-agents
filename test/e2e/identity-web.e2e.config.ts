import { web } from "@e2e-dev/web";
import type { E2EConfig } from "e2e";

const adminURL = process.env.CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL;
if (adminURL === undefined) throw new Error("CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL is required");
const cdpEndpoint = process.env.CLOUD_AGENTS_IDENTITY_E2E_CDP_ENDPOINT;
if (cdpEndpoint === undefined)
  throw new Error("CLOUD_AGENTS_IDENTITY_E2E_CDP_ENDPOINT is required");

export default {
  projectId: "cloud-agents-identity-web",
  tests: ["identity-web.e2e.ts", "identity-account.e2e.ts"],
  output: ".tmp/identity-web",
  workers: 1,
  cache: "off",
  trace: "off",
  targets: [
    {
      name: "identity-web-chromium",
      engine: web({
        connect: { cdpEndpoint: async () => cdpEndpoint },
        viewport: { width: 1440, height: 900 },
      }),
      app: {
        url: adminURL,
        environment: "test",
        identity: "cloud-agents-identity-web",
      },
    },
  ],
} satisfies E2EConfig;
