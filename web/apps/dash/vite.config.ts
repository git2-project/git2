import react from "@vitejs/plugin-react";
import { defineConfig, type ProxyOptions } from "vite";

const api = process.env.GIT2_API_URL ?? "http://localhost:8080";
// Optional: a mock of dash-api generated from contracts/dash-api/openapi.yaml
// (see contracts/dash-api/README.md). Sign-in still goes to the real auth-svc.
const dashMock = process.env.GIT2_DASH_MOCK_URL;

const mockProxy: Record<string, ProxyOptions> = dashMock
  ? { "^/api/(?!auth/)": { target: dashMock, rewrite: (path) => path.replace(/^\/api/, "") } }
  : {};

// Dash is the dev entry point. It proxies /api to the Go server and /admin/ to
// the Admin dev server, so both planes share one origin and one sign-in, the
// same way the gateway will serve them in production.
export default defineConfig({
  plugins: [react()],
  resolve: { dedupe: ["react", "react-dom", "react-router-dom"] },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      // First match wins, so the mock rule must come before the catch-all /api/.
      ...mockProxy,
      "/api/": { target: api },
      "^/admin(/|$)": { target: "http://localhost:5174", ws: true },
    },
  },
});
