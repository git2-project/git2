import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    // Testing Library unmounts between tests only when afterEach is global.
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
  },
});
