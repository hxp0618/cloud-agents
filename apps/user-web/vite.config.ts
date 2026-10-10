import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const webServer = loadEnv(mode, process.cwd(), "").CLOUD_AGENTS_WEB_BFF_URL;
  const proxy = webServer
    ? { "/v1": { target: webServer, changeOrigin: false, secure: true } }
    : undefined;

  return {
    server: {
      host: "127.0.0.1",
      port: 4173,
      strictPort: true,
      ...(proxy === undefined ? {} : { proxy }),
    },
    preview: { host: "127.0.0.1", port: 4173, strictPort: true },
    build: {
      rolldownOptions: {
        output: {
          codeSplitting: {
            groups: [
              {
                name: "react",
                test: /[\\/]node_modules[\\/].*[\\/](react|react-dom|scheduler)[\\/]/,
              },
              { name: "platform-sdk", test: /[\\/]sdk[\\/]typescript[\\/]/ },
            ],
          },
        },
      },
    },
  };
});
