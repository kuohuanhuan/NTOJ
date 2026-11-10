import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tailwindcss(), reactRouter()],
  resolve: { tsconfigPaths: true },
  server: {
    // Same-origin in development too, so cookies and CSRF rules behave as in production.
    proxy: { "/api": process.env.API_URL ?? "http://localhost:8080" },
  },
});
