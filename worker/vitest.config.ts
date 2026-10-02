import path from "node:path";
import { cloudflareTest, readD1Migrations } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    cloudflareTest(async () => ({
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: {
        // Fake secrets and vars, so tests never need a .dev.vars or real keys.
        bindings: {
          TEST_MIGRATIONS: await readD1Migrations(path.join(import.meta.dirname, "migrations")),
          UPLOAD_TOKEN: "test-upload-token",
          PARSE_SECRET: "test-parse-secret",
          R2_ACCESS_KEY_ID: "test-access-key",
          R2_SECRET_ACCESS_KEY: "test-secret-key",
          R2_ACCOUNT_ID: "testaccount",
          R2_BUCKET: "demoview-test",
          PARSER_BUILD: "test-build",
        },
      },
    })),
  ],
  test: {
    setupFiles: ["./test/setup.ts"],
  },
});
