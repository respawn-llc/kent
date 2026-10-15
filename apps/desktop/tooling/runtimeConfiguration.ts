import { readFileSync } from "node:fs";
import { dirname, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { z } from "zod";

type RuntimeAlias = Readonly<{
  find: string;
  replacement: string;
  shape: "exact" | "prefix";
}>;

const runtimeAliases: readonly RuntimeAlias[] = [
  {
    find: "@app/server-api-contract/gen",
    replacement: fileURLToPath(new URL("../packages/server-api-contract/src/gen", import.meta.url)),
    shape: "prefix",
  },
  {
    find: "@app/server-api-contract",
    replacement: fileURLToPath(new URL("../packages/server-api-contract/src/index.ts", import.meta.url)),
    shape: "exact",
  },
  {
    find: "@",
    replacement: fileURLToPath(new URL("../src", import.meta.url)),
    shape: "prefix",
  },
  {
    find: "@app/native-bridge",
    replacement: fileURLToPath(new URL("../packages/native-bridge/src/index.ts", import.meta.url)),
    shape: "exact",
  },
  {
    find: "@app/ui-kit",
    replacement: fileURLToPath(new URL("../packages/ui-kit/src/index.ts", import.meta.url)),
    shape: "exact",
  },
];

export const viteRuntimeAliases = runtimeAliases.map(({ find, replacement }) => ({
  find,
  replacement,
}));

export function runtimeTypeScriptPaths(configPath: string): Record<string, readonly string[]> {
  return Object.fromEntries(
    runtimeAliases.map(({ find, replacement, shape }) => {
      const importPattern = shape === "prefix" ? `${find}/*` : find;
      const relativeTarget = relative(dirname(configPath), replacement).split(sep).join("/");
      const targetPattern = shape === "prefix" ? `${relativeTarget}/*` : relativeTarget;
      return [importPattern, [targetPattern]];
    }),
  );
}

export const protocolVersion = z
  .object({ version: z.string().min(1) })
  .parse(
    JSON.parse(readFileSync(new URL("../../../shared/protocol/version.json", import.meta.url), "utf8")),
  ).version;
