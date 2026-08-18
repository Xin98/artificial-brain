import path from "node:path";

import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // The server-only guard throws outside Next's react-server condition;
      // tests substitute a stub so guarded modules stay unit-testable.
      "server-only": path.resolve(__dirname, "src/test/server-only-stub.ts"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    // Globals enable @testing-library/react's automatic per-test cleanup.
    globals: true,
  },
});
