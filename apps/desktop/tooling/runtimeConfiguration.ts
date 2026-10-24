import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { z } from "zod";

const runtimeConfig = z
  .object({
    compilerOptions: z.object({ paths: z.record(z.string(), z.tuple([z.string()])) }),
  })
  .parse(JSON.parse(readFileSync(new URL("../tsconfig.json", import.meta.url), "utf8")));

export const viteRuntimeAliases = Object.entries(runtimeConfig.compilerOptions.paths).map(
  ([pattern, [target]]) => ({
    find: withoutWildcard(pattern),
    replacement: fileURLToPath(new URL(`../${withoutWildcard(target)}`, import.meta.url)),
  }),
);

function withoutWildcard(path: string): string {
  return path.endsWith("/*") ? path.slice(0, -2) : path;
}

export const protocolVersion = z
  .object({ version: z.string().min(1) })
  .parse(
    JSON.parse(readFileSync(new URL("../../../shared/protocol/version.json", import.meta.url), "utf8")),
  ).version;
