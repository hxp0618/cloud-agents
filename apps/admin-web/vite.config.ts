import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const webServer = loadEnv(mode, process.cwd(), "").CLOUD_AGENTS_WEB_BFF_URL;
  const proxy = webServer
    ? { "/v1": { target: webServer, changeOrigin: false, secure: true } }
    : undefined;

  return {
    server: {
      host: "127.0.0.1",
      port: 4174,
      strictPort: true,
      ...(proxy === undefined ? {} : { proxy }),
    },
    preview: { host: "127.0.0.1", port: 4174, strictPort: true },
    build: {
      rolldownOptions: {
        output: {
          // Vendor code changes far less often than the Admin app, so keep it cacheable separately.
          codeSplitting: {
            groups: [
              {
                name: "react",
                test: /[\\/]node_modules[\\/].*[\\/](react|react-dom|scheduler)[\\/]/,
              },
              { name: "platform-sdk", test: /[\\/](sdk[\\/]typescript|cloud-agent-protocol)[\\/]/ },
            ],
          },
        },
      },
    },
  };
});
