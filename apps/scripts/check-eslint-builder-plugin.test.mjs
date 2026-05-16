import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

import { builderArchitecture } from "../desktop/eslint-builder-plugin.js";

const desktopRequire = createRequire(
  new URL("../desktop/package.json", import.meta.url),
);
const { RuleTester } = desktopRequire("eslint");

RuleTester.setDefaultConfig({
  languageOptions: {
    ecmaVersion: "latest",
    sourceType: "module",
    parserOptions: {
      ecmaFeatures: {
        jsx: true,
      },
    },
  },
});

test("builder/no-array-index-key rejects index-like keys only", () => {
  const tester = new RuleTester();

  tester.run(
    "builder/no-array-index-key",
    builderArchitecture.rules["no-array-index-key"],
    {
      valid: [
        {
          code: "items.map((item) => <span key={item.id}>{item.label}</span>);",
          filename: "src/components/transcriptRows.tsx",
        },
      ],
      invalid: [
        {
          code: "items.map((item, index) => <span key={index}>{item.label}</span>);",
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "indexKey" }],
        },
        {
          code: "items.map((item, idx) => <span key={idx}>{item.label}</span>);",
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "indexKey" }],
        },
      ],
    },
  );
});

test("builder/no-raw-dto-in-components handles lowercase component files", () => {
  const tester = new RuleTester();

  tester.run(
    "builder/no-raw-dto-in-components",
    builderArchitecture.rules["no-raw-dto-in-components"],
    {
      valid: [
        {
          code: 'import { SessionViewModel } from "../view-models/session";',
          filename: "src/components/transcriptRows.tsx",
        },
        {
          code: 'import { SessionDto } from "../protocol/session";',
          filename: "src/hooks/useSessions.ts",
        },
      ],
      invalid: [
        {
          code: 'import { SessionDto } from "../view-models/session";',
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "rawDto" }],
        },
        {
          code: 'import { Session } from "../protocol/session";',
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "rawDto" }],
        },
      ],
    },
  );
});

test("builder/no-useeffect-data-loading catches React.useEffect and aliased useEffect in components", () => {
  const tester = new RuleTester();

  tester.run(
    "builder/no-useeffect-data-loading",
    builderArchitecture.rules["no-useeffect-data-loading"],
    {
      valid: [
        {
          code: `
          import { useEffect } from "react";
          export function useSessions() {
            useEffect(() => {
              fetch("/api/sessions");
            }, []);
          }
        `,
          filename: "src/hooks/useSessions.tsx",
        },
        {
          code: `
          import { useEffect } from "react";
          export function transcriptRows() {
            useEffect(() => {
              window.setTimeout(() => undefined, 0);
            }, []);
          }
        `,
          filename: "src/components/transcriptRows.tsx",
        },
      ],
      invalid: [
        {
          code: `
          import * as React from "react";
          export function transcriptRows() {
            React.useEffect(() => {
              fetch("/api/sessions");
            }, []);
          }
        `,
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "dataLoading" }],
        },
        {
          code: `
          import { useEffect as useReactEffect } from "react";
          export function transcriptRows() {
            useReactEffect(() => {
              fetch("/api/sessions");
            }, []);
          }
        `,
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "dataLoading" }],
        },
        {
          code: `
          import { nativeBridge as desktopBridge } from "@builder/desktop-native-bridge";
          import { useEffect } from "react";
          export function transcriptRows() {
            useEffect(() => {
              desktopBridge.clipboard.readText();
            }, []);
          }
        `,
          filename: "src/components/transcriptRows.tsx",
          errors: [{ messageId: "dataLoading" }],
        },
      ],
    },
  );
});

test("builder/no-mutable-exports rejects exported let and var", () => {
  const tester = new RuleTester();

  tester.run(
    "builder/no-mutable-exports",
    builderArchitecture.rules["no-mutable-exports"],
    {
      valid: ["export const state = {};"],
      invalid: [
        {
          code: "export let state = {};",
          errors: [{ messageId: "mutableExport" }],
        },
        {
          code: "export var state = {};",
          errors: [{ messageId: "mutableExport" }],
        },
        {
          code: "let state = {}; export { state };",
          errors: [{ messageId: "mutableExport" }],
        },
        {
          code: "let state = {}; export { state as mutableState };",
          errors: [{ messageId: "mutableExport" }],
        },
      ],
    },
  );
});

assert.ok(builderArchitecture.rules["no-array-index-key"]);
