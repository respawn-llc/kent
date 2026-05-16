import { readdir, readFile } from "node:fs/promises";
import { extname, join, relative } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const defaultWorkspaceRoot = fileURLToPath(new URL("..", import.meta.url));
const checkedExtensions = new Set([".ts", ".tsx", ".mts", ".cts"]);
const ignoredDirectories = new Set(["node_modules", "dist", "target", "gen"]);

export async function checkTypeScriptPolicy(workspaceRoot = defaultWorkspaceRoot) {
  const errors = [];
  const sourceFiles = await findTypeScriptFiles(workspaceRoot);

  for (const sourceFile of sourceFiles) {
    const text = await readFile(sourceFile, "utf8");
    if (containsExplicitAny(text)) {
      errors.push(`${displayPath(workspaceRoot, sourceFile)} uses explicit any.`);
    }
  }

  return errors;
}

function containsExplicitAny(text) {
  const withoutComments = stripComments(text);
  return /\bany\b/.test(withoutComments);
}

function stripComments(text) {
  let result = "";
  let index = 0;
  let state = "code";

  while (index < text.length) {
    const char = text[index];
    const next = text[index + 1] ?? "";

    if (state === "code") {
      if (char === "/" && next === "/") {
        state = "line-comment";
        index += 2;
        continue;
      }
      if (char === "/" && next === "*") {
        state = "block-comment";
        index += 2;
        continue;
      }
      if (char === '"' || char === "'" || char === "`") {
        state = char;
        result += " ";
        index += 1;
        continue;
      }
      result += char;
      index += 1;
      continue;
    }

    if (state === "line-comment") {
      if (char === "\n") {
        result += char;
        state = "code";
      }
      index += 1;
      continue;
    }

    if (state === "block-comment") {
      if (char === "\n") {
        result += char;
      }
      if (char === "*" && next === "/") {
        state = "code";
        index += 2;
        continue;
      }
      index += 1;
      continue;
    }

    if (char === "\\") {
      index += 2;
      continue;
    }
    if (char === state) {
      state = "code";
      result += " ";
    }
    index += 1;
  }

  return result;
}

async function findTypeScriptFiles(root) {
  const result = [];
  await visit(root);
  return result.sort();

  async function visit(dir) {
    const entries = await readdir(dir, { withFileTypes: true });
    for (const entry of entries) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (ignoredDirectories.has(entry.name)) {
          continue;
        }
        await visit(path);
        continue;
      }
      if (entry.isFile() && checkedExtensions.has(extname(entry.name))) {
        result.push(path);
      }
    }
  }
}

function displayPath(workspaceRoot, path) {
  return relative(workspaceRoot, path);
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const errors = await checkTypeScriptPolicy();
  if (errors.length > 0) {
    console.error("TypeScript policy failed:");
    for (const error of errors) {
      console.error(`- ${error}`);
    }
    process.exit(1);
  }
}
