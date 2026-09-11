import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, extname, join, relative } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { appArchitecture } from "../desktop/eslint-app-plugin.js";

const root = fileURLToPath(new URL("../..", import.meta.url));
const desktopRequire = createRequire(
  new URL("../desktop/package.json", import.meta.url),
);
const docsRequire = createRequire(
  new URL("../../docs/package.json", import.meta.url),
);
const astroRequire = createRequire(docsRequire.resolve("astro/package.json"));
const { ESLint } = desktopRequire("eslint");
const tseslint = desktopRequire("typescript-eslint");
const ts = desktopRequire("typescript");
const sourceExtensions = new Set([
  ".ts",
  ".tsx",
  ".mts",
  ".cts",
  ".js",
  ".jsx",
  ".mjs",
  ".cjs",
]);
const embeddedExtensions = new Set([".html", ".astro"]);

export function effectPolicyConfig(programs = undefined) {
  return [
    {
      files: ["**/*.{ts,tsx,mts,cts,js,jsx,mjs,cjs}"],
      languageOptions: {
        parser: tseslint.parser,
        parserOptions: { programs, ecmaFeatures: { jsx: true } },
      },
      linterOptions: { noInlineConfig: true },
      plugins: { app: appArchitecture },
      rules: {
        "app/no-unowned-effect": "error",
        ...(programs === undefined
          ? {}
          : { "app/no-effect-subscriptions": "error" }),
      },
    },
  ];
}

function repositorySources() {
  return execFileSync(
    "git",
    ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
    {
      cwd: root,
      encoding: "utf8",
    },
  )
    .split("\0")
    .filter((path) => {
      const parts = path.split("/");
      return (
        path.length > 0 &&
        parts[0] !== "tui-rs" &&
        !parts.includes("eslint-fixtures") &&
        (sourceExtensions.has(extname(path)) ||
          embeddedExtensions.has(extname(path))) &&
        existsSync(join(root, path))
      );
    })
    .map((path) => join(root, path));
}

async function embeddedSources(path) {
  const text = readFileSync(path, "utf8");
  if (extname(path) === ".astro")
    return [
      {
        path: `${path}.tsx`,
        code: astroRequire("@astrojs/compiler/sync").convertToTSX(text, {
          filename: path,
        }).code,
      },
    ];
  const { fromHtml } = await import(
    pathToFileURL(docsRequire.resolve("hast-util-from-html")).href
  );
  const tree = fromHtml(text);
  const scripts = [];
  function visit(node) {
    if (node.type === "element" && (node.name ?? node.tagName) === "script") {
      scripts.push(node.children.map((child) => child.value ?? "").join(""));
      return;
    }
    for (const child of node.children ?? []) visit(child);
  }
  visit(tree);
  return scripts.map((code, index) => ({
    path: `${path}.${index.toString()}.ts`,
    code,
  }));
}

export async function checkEffectPolicy(paths = repositorySources()) {
  const ordinary = paths.filter((path) => sourceExtensions.has(extname(path)));
  const virtual = new Map();
  for (const path of paths.filter((file) =>
    embeddedExtensions.has(extname(file)),
  )) {
    for (const script of await embeddedSources(path))
      virtual.set(script.path, script.code);
  }
  const configPath = join(root, "apps/desktop/tsconfig.app.json");
  const config = ts.readConfigFile(configPath, ts.sys.readFile);
  if (config.error !== undefined)
    throw new Error(
      ts.flattenDiagnosticMessageText(config.error.messageText, "\n"),
    );
  const parsed = ts.parseJsonConfigFileContent(
    config.config,
    ts.sys,
    dirname(configPath),
  );
  const options = { ...parsed.options, allowJs: true, noEmit: true };
  const host = ts.createCompilerHost(options);
  const getSourceFile = host.getSourceFile;
  host.getSourceFile = (
    path,
    languageVersion,
    onError,
    shouldCreateNewSourceFile,
  ) =>
    virtual.has(path)
      ? ts.createSourceFile(path, virtual.get(path), languageVersion, true)
      : getSourceFile(
          path,
          languageVersion,
          onError,
          shouldCreateNewSourceFile,
        );
  const program = ts.createProgram(
    [...ordinary, ...virtual.keys()],
    options,
    host,
  );
  const eslint = new ESLint({
    cwd: root,
    overrideConfigFile: true,
    ignore: false,
    overrideConfig: effectPolicyConfig([program]),
  });
  const results = ordinary.length === 0 ? [] : await eslint.lintFiles(ordinary);
  for (const [path, code] of virtual) {
    const scriptResults = await eslint.lintText(code, { filePath: path });
    results.push(...scriptResults);
  }
  return results;
}

if (
  process.argv[1] !== undefined &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const inputs = process.argv.slice(2);
  const results = await checkEffectPolicy(
    inputs.length === 0 ? undefined : inputs.map((path) => join(root, path)),
  );
  for (const result of results) {
    for (const message of result.messages) {
      console.error(
        `${relative(root, result.filePath)}:${message.line}:${message.column} ${message.message} (${message.ruleId ?? "parser"})`,
      );
    }
    if (result.errorCount > 0) process.exitCode = 1;
  }
}
