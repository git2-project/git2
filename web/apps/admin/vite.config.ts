import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const api = process.env.GIT2_API_URL ?? "http://localhost:8080";

// Admin is served under /admin/. In development open it through the Dash dev
// server (http://localhost:5173/admin/) so that it shares Dash's origin and
// session; its own port only serves the files and hot reload.
export default defineConfig({
  base: "/admin/",
  plugins: [react()],
  resolve: { dedupe: ["react", "react-dom", "react-router-dom"] },
  server: {
    port: 5174,
    strictPort: true,
    hmr: { clientPort: 5174 },
    proxy: { "/api/": { target: api } },
  },
});
