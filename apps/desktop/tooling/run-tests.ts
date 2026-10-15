import { readdir, mkdtemp, rmdir, stat, unlink, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { tmpdir } from "node:os";
import { basename, isAbsolute, join, relative, resolve, sep } from "node:path";
import { runtimeTypeScriptPaths, protocolVersion } from "./runtimeConfiguration";

const desktopRoot = process.cwd();
const [workerArgument, ...testArguments] = process.argv.slice(2);
const workerCount = workerArgument?.startsWith("--workers=")
  ? Number(workerArgument.slice("--workers=".length))
  : undefined;
if (workerCount === undefined || !Number.isSafeInteger(workerCount) || workerCount < 1) {
  throw new Error("Desktop test launch requires a positive Just worker count.");
}

const testRoots = ["src", "packages", "tooling"];
const plainTests = await discoverTests(testRoots, ".test.ts");
const browserTests = await discoverTests(testRoots, ".test.tsx");
const { selectors, runnerArguments } = splitTestArguments(testArguments);
const selectedTests = await selectTests([...plainTests, ...browserTests], selectors);
const temporaryDirectory = await mkdtemp(join(tmpdir(), "kent-desktop-tests-"));
const tsconfigPath = join(temporaryDirectory, "tsconfig.json");
const tsconfig = {
  compilerOptions: {
    jsx: "react-jsx",
    jsxImportSource: "react",
    paths: runtimeTypeScriptPaths(tsconfigPath),
  },
};

let failureCode = 0;
let configCreated = false;
try {
  await writeFile(tsconfigPath, JSON.stringify(tsconfig));
  configCreated = true;
  for (const environment of [
    {
      tests: selectedTests.filter((path) => path.endsWith(".test.ts")),
      preloads: ["./test/commonSetup.ts"],
    },
    {
      tests: selectedTests.filter((path) => path.endsWith(".test.tsx")),
      preloads: ["./test/commonSetup.ts", "./test/setup.ts"],
    },
  ]) {
    if (environment.tests.length === 0) continue;
    const testFiles = environment.tests.map(
      (path) => `./${relative(desktopRoot, path).split(sep).join("/")}`,
    );
    const exitCode = await runBunTest([
      `--parallel=${workerCount.toString()}`,
      "--isolate",
      `--tsconfig-override=${tsconfigPath}`,
      ...environment.preloads.map((preload) => `--preload=${preload}`),
      `--define=__KENT_PROTOCOL_VERSION__=${JSON.stringify(protocolVersion)}`,
      "--define=import.meta.env=globalThis.__KENT_TEST_ENV__",
      ...runnerArguments,
      ...testFiles,
    ]);
    if (exitCode !== 0 && failureCode === 0) {
      failureCode = exitCode;
    }
  }
} finally {
  if (configCreated) await unlink(tsconfigPath);
  await rmdir(temporaryDirectory);
}

process.exitCode = failureCode;

async function discoverTests(roots: readonly string[], suffix: string): Promise<string[]> {
  const files: string[] = [];
  async function visit(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true });
    for (const entry of entries) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "dist" && entry.name !== "gen" && entry.name !== "node_modules") {
          await visit(path);
        }
      } else if (entry.isFile() && entry.name.endsWith(suffix)) {
        files.push(path);
      }
    }
  }

  for (const root of roots) {
    await visit(join(desktopRoot, root));
  }
  return files.sort();
}

function splitTestArguments(arguments_: readonly string[]): Readonly<{
  runnerArguments: string[];
  selectors: string[];
}> {
  const optionsWithValues = new Set([
    "-t",
    "--bail",
    "--coverage-dir",
    "--coverage-reporter",
    "--max-concurrency",
    "--parallel",
    "--parallel-delay",
    "--path-ignore-patterns",
    "--reporter",
    "--reporter-outfile",
    "--rerun-each",
    "--retry",
    "--seed",
    "--shard",
    "--test-name-pattern",
    "--timeout",
    "--timings",
  ]);
  const runnerArguments: string[] = [];
  const selectors: string[] = [];
  for (let index = 0; index < arguments_.length; index += 1) {
    const argument = arguments_[index];
    if (argument === undefined) {
      continue;
    }
    if (!argument.startsWith("-")) {
      selectors.push(argument);
      continue;
    }
    runnerArguments.push(argument);
    if (argument.includes("=") || !optionsWithValues.has(argument)) continue;
    const value = arguments_[index + 1];
    if (value === undefined) {
      throw new Error(`Desktop test option ${argument} requires a value.`);
    }
    runnerArguments.push(value);
    index += 1;
  }
  return { runnerArguments, selectors };
}

async function selectTests(allTests: readonly string[], selectors: readonly string[]): Promise<string[]> {
  if (selectors.length === 0) return [...allTests];
  const selectedTests = new Set<string>();
  for (const selector of selectors) {
    await addSelectorMatches(allTests, selectedTests, selector);
  }
  if (selectedTests.size === 0) {
    throw new Error(`No Desktop test files matched: ${selectors.join(", ")}`);
  }
  return [...selectedTests].sort();
}

async function addSelectorMatches(
  allTests: readonly string[],
  selectedTests: Set<string>,
  selector: string,
): Promise<void> {
  const absoluteSelector = resolve(desktopRoot, selector);
  const selectorInfo = await readPathInfo(absoluteSelector);
  if (selectorInfo?.isDirectory()) {
    addDirectoryTests(allTests, selectedTests, absoluteSelector);
  } else if (selectorInfo?.isFile()) {
    if (allTests.includes(absoluteSelector)) selectedTests.add(absoluteSelector);
  } else if (basename(selector) === selector) {
    for (const testPath of allTests) {
      if (basename(testPath) === selector) selectedTests.add(testPath);
    }
  }
}

async function readPathInfo(path: string) {
  try {
    return await stat(path);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return undefined;
    throw error;
  }
}

function addDirectoryTests(allTests: readonly string[], selectedTests: Set<string>, directory: string): void {
  for (const testPath of allTests) {
    const pathFromDirectory = relative(directory, testPath);
    if (isWithinDirectory(pathFromDirectory)) selectedTests.add(testPath);
  }
}

function isWithinDirectory(pathFromDirectory: string): boolean {
  return (
    pathFromDirectory.length > 0 &&
    pathFromDirectory !== ".." &&
    !pathFromDirectory.startsWith(`..${sep}`) &&
    !isAbsolute(pathFromDirectory)
  );
}

async function runBunTest(arguments_: readonly string[]): Promise<number> {
  return new Promise((resolveExitCode, reject) => {
    const child = spawn(process.execPath, ["test", ...arguments_], {
      cwd: desktopRoot,
      stdio: "inherit",
    });
    child.once("error", reject);
    child.once("close", (exitCode) => {
      resolveExitCode(exitCode ?? 1);
    });
  });
}
