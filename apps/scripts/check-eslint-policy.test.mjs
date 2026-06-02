import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

import eslintConfig from "../desktop/eslint.config.js";

const desktopRequire = createRequire(new URL("../desktop/package.json", import.meta.url));
const { ESLint } = desktopRequire("eslint");

test("desktop ESLint config explicitly forbids explicit any", () => {
  const rule = findRule("@typescript-eslint/no-explicit-any");

  assert.equal(rule, "error");
});

test("desktop ESLint config explicitly enables type-aware async and unsafe-value rules", () => {
  assert.equal(findRule("@typescript-eslint/no-floating-promises"), "error");
  assert.equal(findRule("@typescript-eslint/no-unsafe-assignment"), "error");
  assert.equal(findRule("@typescript-eslint/no-unsafe-call"), "error");
  assert.equal(findRule("@typescript-eslint/no-unsafe-member-access"), "error");
  assert.equal(findRule("@typescript-eslint/promise-function-async"), "error");
  assert.equal(findRule("@typescript-eslint/require-await"), "off");
  assert.deepEqual(findRule("@typescript-eslint/return-await"), ["error", "in-try-catch"]);
});

test("desktop ESLint config explicitly forbids unsafe type assertions", () => {
  assert.deepEqual(findRule("@typescript-eslint/consistent-type-assertions"), [
    "error",
    {
      assertionStyle: "never",
    },
  ]);
});

test("desktop ESLint config explicitly forbids direct Tauri imports", () => {
  const rule = findRule("no-restricted-imports");

  assert.ok(Array.isArray(rule));
  assert.equal(rule[0], "error");

  const options = rule[1];
  assert.ok(options !== null && typeof options === "object");

  const paths = Array.isArray(options.paths) ? options.paths : [];
  assert.ok(paths.some((entry) => entry.name === "@tauri-apps/api"));

  const patterns = Array.isArray(options.patterns) ? options.patterns : [];
  assert.ok(
    patterns.some((entry) => Array.isArray(entry.group) && entry.group.includes("@tauri-apps/api/*")),
  );
});

test("desktop ESLint config explicitly enforces GUI architecture rules", () => {
  assert.equal(findRule("builder/no-array-index-key"), "error");
  assert.equal(findRule("builder/no-mutable-exports"), "error");
  assert.equal(findRule("builder/no-raw-dto-in-components"), "error");
  assert.equal(findRule("builder/no-useeffect-data-loading"), "error");
});

test("desktop ESLint architecture rules reject representative component violations", async () => {
  const messages = await lintWithBuilderArchitectureRules(`
    import { useEffect as useReactEffect } from "react";
    import { SessionDto } from "../protocol/session";
    export let mutableSessionCount = 0;

    export function transcriptRows({ items }) {
      useReactEffect(() => {
        fetch("/api/sessions");
      }, []);

      return items.map((item, index) => <span key={index}>{item}</span>);
    }
  `);

  assert.deepEqual(
    messages.map((message) => message.ruleId).sort(),
    [
      "builder/no-array-index-key",
      "builder/no-mutable-exports",
      "builder/no-raw-dto-in-components",
      "builder/no-useeffect-data-loading",
    ],
  );
});

test("desktop ESLint config explicitly enforces complexity and debug-output limits", () => {
  assert.deepEqual(findRule("complexity"), ["error", { max: 12 }]);
  assert.deepEqual(findRule("max-depth"), ["error", 4]);
  assert.deepEqual(findRuleForFiles("max-lines", "**/*.{ts,tsx}"), [
    "error",
    { max: 650, skipBlankLines: true, skipComments: true },
  ]);
  assert.deepEqual(findRuleForFiles("max-lines", "**/*.test.{ts,tsx}"), [
    "error",
    { max: 1100, skipBlankLines: true, skipComments: true },
  ]);
  assert.deepEqual(findRule("max-params"), ["error", 4]);
  assert.equal(findRule("no-console"), "error");
});

function findRule(name) {
  let result;
  for (const configEntry of eslintConfig) {
    if (configEntry.rules !== undefined && Object.hasOwn(configEntry.rules, name)) {
      result = configEntry.rules[name];
    }
  }
  return result;
}

function findRuleForFiles(name, files) {
  for (const configEntry of eslintConfig) {
    if (arrayEqual(configEntry.files, [files]) && configEntry.rules !== undefined && Object.hasOwn(configEntry.rules, name)) {
      return configEntry.rules[name];
    }
  }
  return undefined;
}

function arrayEqual(left, right) {
  return Array.isArray(left) && left.length === right.length && left.every((item, index) => item === right[index]);
}

async function lintWithBuilderArchitectureRules(source) {
  const builderPlugin = findBuilderPlugin();
  const eslint = new ESLint({
    overrideConfigFile: true,
    overrideConfig: [
      {
        files: ["**/*.tsx"],
        languageOptions: {
          ecmaVersion: "latest",
          sourceType: "module",
          parserOptions: {
            ecmaFeatures: {
              jsx: true,
            },
          },
        },
        plugins: {
          builder: builderPlugin,
        },
        rules: {
          "builder/no-array-index-key": "error",
          "builder/no-mutable-exports": "error",
          "builder/no-raw-dto-in-components": "error",
          "builder/no-useeffect-data-loading": "error",
        },
      },
    ],
  });

  const [result] = await eslint.lintText(source, {
    filePath: "src/components/transcriptRows.tsx",
  });

  return result.messages;
}

function findBuilderPlugin() {
  for (const configEntry of eslintConfig) {
    if (configEntry.plugins?.builder !== undefined) {
      return configEntry.plugins.builder;
    }
  }

  throw new Error("desktop ESLint config is missing builder plugin.");
}
