import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const controlPlane = loadEnv(mode, process.cwd(), "").CLOUD_AGENTS_CONTROL_PLANE_URL;
  const proxy = controlPlane
    ? { "/v1/admin": { target: controlPlane, changeOrigin: false, secure: true } }
    : undefined;

  return {
    server: {
      host: "127.0.0.1",
      port: 4174,
      strictPort: true,
      ...(proxy === undefined ? {} : { proxy }),
    },
    preview: { host: "127.0.0.1", port: 4174, strictPort: true },
  };
});
