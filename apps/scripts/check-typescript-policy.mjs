import { readdir, readFile } from "node:fs/promises";
import { extname, join, relative } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const defaultWorkspaceRoot = fileURLToPath(new URL("..", import.meta.url));
const checkedExtensions = new Set([".ts", ".tsx", ".mts", ".cts"]);
const ignoredDirectories = new Set(["node_modules", "dist", "target", "gen"]);

export async function checkTypeScriptPolicy(
  workspaceRoot = defaultWorkspaceRoot,
) {
  const errors = [];
  const sourceFiles = await findTypeScriptFiles(workspaceRoot);

  for (const sourceFile of sourceFiles) {
    const text = await readFile(sourceFile, "utf8");
    if (containsExplicitAny(text)) {
      errors.push(
        `${displayPath(workspaceRoot, sourceFile)} uses explicit any.`,
      );
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
  const states = ["code"];
  const expressionDepths = [];
  let stringQuote = "";

  while (index < text.length) {
    const char = text[index];
    const next = text[index + 1] ?? "";
    const state = states.at(-1);

    if (state === "code") {
      if (char === "/" && next === "/") {
        states.push("line-comment");
        index += 2;
        continue;
      }
      if (char === "/" && next === "*") {
        states.push("block-comment");
        index += 2;
        continue;
      }
      if (char === '"' || char === "'") {
        stringQuote = char;
        states.push("string");
        result += " ";
        index += 1;
        continue;
      }
      if (char === "`") {
        states.push("template");
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
        states.pop();
      }
      index += 1;
      continue;
    }

    if (state === "block-comment") {
      if (char === "\n") {
        result += char;
      }
      if (char === "*" && next === "/") {
        states.pop();
        index += 2;
        continue;
      }
      index += 1;
      continue;
    }

    if (state === "string") {
      if (char === "\\") {
        index += 2;
        continue;
      }
      if (char === stringQuote) {
        states.pop();
        result += " ";
      }
      index += 1;
      continue;
    }

    if (state === "template") {
      if (char === "$" && next === "{") {
        states.push("template-expression");
        expressionDepths.push(1);
        result += "  ";
        index += 2;
        continue;
      }
      if (char === "\\") {
        index += 2;
        continue;
      }
      if (char === "`") {
        states.pop();
        result += " ";
      }
      index += 1;
      continue;
    }

    if (state === "template-expression") {
      if (char === "/" && next === "/") {
        states.push("line-comment");
        index += 2;
        continue;
      }
      if (char === "/" && next === "*") {
        states.push("block-comment");
        index += 2;
        continue;
      }
      if (char === '"' || char === "'") {
        stringQuote = char;
        states.push("string");
        result += " ";
        index += 1;
        continue;
      }
      if (char === "`") {
        states.push("template");
        result += " ";
        index += 1;
        continue;
      }
      if (char === "{") {
        expressionDepths[expressionDepths.length - 1] += 1;
      }
      if (char === "}") {
        expressionDepths[expressionDepths.length - 1] -= 1;
        if (expressionDepths.at(-1) === 0) {
          expressionDepths.pop();
          states.pop();
          result += " ";
          index += 1;
          continue;
        }
      }
      result += char;
      index += 1;
      continue;
    }
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
