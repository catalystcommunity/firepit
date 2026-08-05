// Do not lint generated code or the vendored CSIL transport.
import js from "@eslint/js";
import solid from "eslint-plugin-solid";
import globals from "globals";
import tseslint from "typescript-eslint";

const solidTypescript = solid.configs["flat/typescript"];

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "src/gen/**", "src/transport/csil/**"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: solidTypescript.plugins,
    rules: {
      ...solidTypescript.rules,
      // The codebase prefers a leading underscore over silencing every
      // unused destructured/callback param individually.
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
    },
    languageOptions: {
      globals: globals.browser,
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
  },
  {
    files: ["src/**/*.test.{ts,tsx}", "vitest.setup.ts"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.vitest },
    },
  },
  {
    files: ["*.config.{ts,js}"],
    languageOptions: {
      globals: globals.node,
    },
  },
);
