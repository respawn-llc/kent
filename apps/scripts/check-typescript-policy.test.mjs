import assert from "node:assert/strict";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { checkTypeScriptPolicy } from "./check-typescript-policy.mjs";

test("rejects explicit any annotations", async () => {
  const workspaceRoot = await makeWorkspace({
    "app/src/file.ts": "const value: any = 1;\n",
  });

  assert.deepEqual(await checkTypeScriptPolicy(workspaceRoot), ["app/src/file.ts uses explicit any."]);
});

test("rejects as any casts", async () => {
  const workspaceRoot = await makeWorkspace({
    "shared/file.ts": "const value = unknownValue as any;\n",
  });

  assert.deepEqual(await checkTypeScriptPolicy(workspaceRoot), ["shared/file.ts uses explicit any."]);
});

test("ignores comments and string literals", async () => {
  const workspaceRoot = await makeWorkspace({
    "app/src/file.ts": "const text = 'any';\n// any in comment\n/* any in block */\n",
  });

  assert.deepEqual(await checkTypeScriptPolicy(workspaceRoot), []);
});

async function makeWorkspace(files) {
  const workspaceRoot = await mkdtemp(join(tmpdir(), "builder-ts-policy-"));
  for (const [path, content] of Object.entries(files)) {
    const filePath = join(workspaceRoot, path);
    await mkdir(join(filePath, ".."), { recursive: true });
    await writeFile(filePath, content);
  }
  return workspaceRoot;
}
