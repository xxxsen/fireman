// ESLint 9 flat config for the Fireman web project. Stage 1 keeps the rule
// set minimal: it must pass cleanly in CI without pulling browser
// automation tooling. Richer Next-aware presets can be added in later stages
// once the application surface area grows.

import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      "node_modules/**",
      "node_modules.bak/**",
      "node_modules.next.broken/**",
      ".next/**",
      "next-env.d.ts",
      "out/**",
      "coverage/**",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx,js,jsx,mjs,cjs}"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
        React: "readonly",
      },
    },
    rules: {
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },
  {
    files: ["**/*.test.{ts,tsx}", "vitest.setup.ts", "vitest.config.ts"],
    languageOptions: {
      globals: {
        ...globals.node,
        vi: "readonly",
        describe: "readonly",
        it: "readonly",
        test: "readonly",
        expect: "readonly",
        beforeAll: "readonly",
        beforeEach: "readonly",
        afterAll: "readonly",
        afterEach: "readonly",
      },
    },
  },
);
