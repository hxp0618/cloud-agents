import { web } from "@e2e-dev/web";
import type { E2EConfig } from "e2e";

export default {
  projectId: "cloud-agents-admin-web",
  tests: ["admin-web.e2e.ts", "admin-agent-events.e2e.ts"],
  output: ".tmp",
  workers: 1,
  cache: "off",
  trace: "retain-on-failure",
  targets: [
    {
      name: "admin-web-chromium",
      engine: web({ locale: "en-US" }),
      app: {
        url: "http://127.0.0.1:0",
        command: {
          executable: "bun",
          cwd: "../../apps/admin-web",
          args: ["run", "dev", "--", "--host", "127.0.0.1", "--port", "{port}"],
        },
      },
    },
  ],
} satisfies E2EConfig;
