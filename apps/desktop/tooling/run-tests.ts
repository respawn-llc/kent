import { mkdtemp, rmdir, unlink, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { tmpdir } from "node:os";
import { format, join, parse } from "node:path";
import { runtimeTypeScriptPaths, protocolVersion } from "./runtimeConfiguration";

const desktopRoot = process.cwd();
const [workerArgument, ...testArguments] = process.argv.slice(2);
const workerCount = workerArgument?.startsWith("--workers=")
  ? Number(workerArgument.slice("--workers=".length))
  : undefined;
if (workerCount === undefined || !Number.isSafeInteger(workerCount) || workerCount < 1) {
  throw new Error("Desktop test launch requires a positive Just worker count.");
}

const commonPathIgnorePatterns = [
  "test/**",
  "eslint-fixtures/**",
  "**/node_modules/**",
  "**/dist/**",
  "**/gen/**",
  "**/*.spec.*",
  "**/*_spec.*",
  "**/*_test.*",
  "**/*.test.js",
  "**/*.test.jsx",
  "**/*.test.mjs",
  "**/*.test.cjs",
  "**/*.test.mts",
  "**/*.test.cts",
] as const;
const environments = [
  { name: "plain", preloads: ["./test/commonSetup.ts"], excludedPatterns: ["**/*.test.tsx"] },
  {
    name: "browser",
    preloads: ["./test/commonSetup.ts", "./test/setup.ts"],
    excludedPatterns: ["**/*.test.ts"],
  },
] as const;
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
  for (const environment of environments) {
    const exitCode = await runBunTest([
      `--parallel=${workerCount.toString()}`,
      "--isolate",
      `--tsconfig-override=${tsconfigPath}`,
      ...environment.preloads.map((preload) => `--preload=${preload}`),
      `--define=__KENT_PROTOCOL_VERSION__=${JSON.stringify(protocolVersion)}`,
      "--define=import.meta.env=globalThis.__KENT_TEST_ENV__",
      ...commonPathIgnorePatterns.map((pattern) => `--path-ignore-patterns=${pattern}`),
      ...environment.excludedPatterns.map((pattern) => `--path-ignore-patterns=${pattern}`),
      "--pass-with-no-tests",
      ...callerArgumentsForEnvironment(testArguments, environment.name),
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

function callerArgumentsForEnvironment(
  arguments_: readonly string[],
  environment: (typeof environments)[number]["name"],
): string[] {
  const result: string[] = [];
  for (let index = 0; index < arguments_.length; index += 1) {
    const argument = arguments_[index];
    if (argument === undefined) continue;
    if (argument === "--reporter-outfile") {
      const path = arguments_[index + 1];
      if (path === undefined) {
        result.push(argument);
        continue;
      }
      result.push(argument, environmentReportPath(path, environment));
      index += 1;
      continue;
    }
    const reporterOutfilePrefix = "--reporter-outfile=";
    if (argument.startsWith(reporterOutfilePrefix)) {
      const path = argument.slice(reporterOutfilePrefix.length);
      result.push(
        path.length === 0 ? argument : `${reporterOutfilePrefix}${environmentReportPath(path, environment)}`,
      );
      continue;
    }
    result.push(argument);
  }
  return result;
}

function environmentReportPath(path: string, environment: (typeof environments)[number]["name"]): string {
  const parsedPath = parse(path);
  return format({
    root: parsedPath.root,
    dir: parsedPath.dir,
    name: `${parsedPath.name}.${environment}`,
    ext: parsedPath.ext,
  });
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
