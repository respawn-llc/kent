import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import testingLibrary from "eslint-plugin-testing-library";
import tseslint from "typescript-eslint";

import { builderArchitecture } from "./eslint-builder-plugin.js";

const tauriImportRestriction = {
  paths: [
    {
      name: "@tauri-apps/api",
      message: "Import Tauri APIs only inside a NativeBridge package.",
    },
  ],
  patterns: [
    {
      group: ["@tauri-apps/api/*"],
      message: "Import Tauri APIs only inside a NativeBridge package.",
    },
  ],
};

export default tseslint.config(
  {
    ignores: ["**/dist", "src-tauri/target", "node_modules"],
  },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parserOptions: {
        project: ["./tsconfig.app.json", "./tsconfig.node.json", "./packages/native-bridge/tsconfig.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      builder: builderArchitecture,
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "@typescript-eslint/consistent-type-imports": ["error", { fixStyle: "inline-type-imports" }],
      "@typescript-eslint/consistent-type-assertions": [
        "error",
        {
          assertionStyle: "never",
        },
      ],
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/no-unsafe-assignment": "error",
      "@typescript-eslint/no-unsafe-call": "error",
      "@typescript-eslint/no-unsafe-member-access": "error",
      "@typescript-eslint/no-misused-promises": [
        "error",
        {
          checksVoidReturn: {
            attributes: false,
          },
        },
      ],
      "@typescript-eslint/promise-function-async": "error",
      "@typescript-eslint/require-await": "off",
      "@typescript-eslint/return-await": ["error", "in-try-catch"],
      "@typescript-eslint/switch-exhaustiveness-check": "error",
      "builder/no-array-index-key": "error",
      "builder/no-mutable-exports": "error",
      "builder/no-raw-dto-in-components": "error",
      "builder/no-useeffect-data-loading": "error",
      complexity: ["error", { max: 12 }],
      "max-depth": ["error", 4],
      "max-lines": ["error", { max: 650, skipBlankLines: true, skipComments: true }],
      "max-params": ["error", 4],
      "no-console": "error",
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": ["error", tauriImportRestriction],
    },
  },
  {
    files: ["src/**/*.{tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          ...tauriImportRestriction,
          paths: [
            ...tauriImportRestriction.paths,
            {
              name: "@builder/desktop-native-bridge",
              message:
                "Components must use app services or feature hooks instead of importing NativeBridge directly.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["**/*.{tsx}"],
    ...jsxA11y.flatConfigs.recommended,
  },
  {
    files: ["**/*.test.{ts,tsx}"],
    ...testingLibrary.configs["flat/react"],
    rules: {
      ...testingLibrary.configs["flat/react"].rules,
      "max-lines": ["error", { max: 1100, skipBlankLines: true, skipComments: true }],
    },
  },
  prettier,
);
