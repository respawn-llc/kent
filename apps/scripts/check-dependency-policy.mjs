import { readdir, readFile } from "node:fs/promises";
import { join, relative } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const defaultWorkspaceRoot = fileURLToPath(new URL("..", import.meta.url));
const dependencySections = [
  "dependencies",
  "devDependencies",
  "peerDependencies",
  "optionalDependencies",
];

export async function checkDependencyPolicy(
  workspaceRoot = defaultWorkspaceRoot,
) {
  const policyPath = join(workspaceRoot, "dependency-policy.json");
  const bunfigPath = join(workspaceRoot, "bunfig.toml");
  const rootPackagePath = join(workspaceRoot, "package.json");
  const policy = JSON.parse(await readFile(policyPath, "utf8"));
  const bunfig = Bun.TOML.parse(await readFile(bunfigPath, "utf8"));
  const rootPackage = JSON.parse(await readFile(rootPackagePath, "utf8"));
  const installConfig = bunfig.install ?? {};
  const errors = [];

  if (installConfig.minimumReleaseAge !== policy.minimumReleaseAgeSeconds) {
    errors.push(
      `bunfig.toml must set install.minimumReleaseAge to ${policy.minimumReleaseAgeSeconds} seconds to enforce dependency maturity.`,
    );
  }

  if (
    !sameSortedValues(
      installConfig.minimumReleaseAgeExcludes,
      policy.minimumReleaseAgeExcludes,
    )
  ) {
    errors.push(
      "bunfig.toml install.minimumReleaseAgeExcludes must match the approved package-name exceptions in dependency-policy.json.",
    );
  }

  if (
    !Array.isArray(rootPackage.trustedDependencies) ||
    rootPackage.trustedDependencies.length > 0
  ) {
    errors.push(
      "apps/package.json must keep trustedDependencies empty so install scripts require explicit review.",
    );
  }

  const packagePaths = await findPackageJsonFiles(workspaceRoot);
  const packagesByName = new Map();

  for (const packagePath of packagePaths) {
    const packageJson = JSON.parse(await readFile(packagePath, "utf8"));
    const packageName = packageJson.name;
    if (typeof packageName !== "string" || packageName.length === 0) {
      errors.push(
        `${displayPath(workspaceRoot, packagePath)} is missing package name.`,
      );
      continue;
    }
    packagesByName.set(packageName, packagePath);

    const packagePolicy = policy.directDependencyAllowlist[packageName];
    if (packagePolicy === undefined) {
      errors.push(
        `${packageName} has no direct dependency allowlist in dependency-policy.json.`,
      );
      continue;
    }

    for (const section of dependencySections) {
      const actualDependencies = Object.keys(packageJson[section] ?? {}).sort();
      const allowedDependencies = [...(packagePolicy[section] ?? [])].sort();

      for (const dependencyName of actualDependencies) {
        if (!allowedDependencies.includes(dependencyName)) {
          errors.push(
            `${packageName} ${section} contains unreviewed dependency ${dependencyName}.`,
          );
        }
      }

      for (const dependencyName of allowedDependencies) {
        if (!actualDependencies.includes(dependencyName)) {
          errors.push(
            `${packageName} policy allowlists absent ${section} dependency ${dependencyName}.`,
          );
        }
      }
    }
  }

  for (const packageName of Object.keys(
    policy.directDependencyAllowlist,
  ).sort()) {
    if (!packagesByName.has(packageName)) {
      errors.push(
        `dependency-policy.json contains package ${packageName}, but no matching package.json exists.`,
      );
    }
  }

  return errors;
}

async function findPackageJsonFiles(root) {
  const result = [];
  await visit(root);
  return result.sort();

  async function visit(dir) {
    const entries = await readdir(dir, { withFileTypes: true });
    for (const entry of entries) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (["node_modules", "dist", "target", "gen"].includes(entry.name)) {
          continue;
        }
        await visit(path);
        continue;
      }
      if (entry.isFile() && entry.name === "package.json") {
        result.push(path);
      }
    }
  }
}

function displayPath(workspaceRoot, path) {
  return relative(workspaceRoot, path);
}

function sameSortedValues(left, right) {
  if (!Array.isArray(left) || !Array.isArray(right)) {
    return false;
  }
  const sortedLeft = [...left].sort();
  const sortedRight = [...right].sort();
  return (
    sortedLeft.length === sortedRight.length &&
    sortedLeft.every((value, index) => value === sortedRight[index])
  );
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const errors = await checkDependencyPolicy();
  if (errors.length > 0) {
    console.error("Frontend dependency policy failed:");
    for (const error of errors) {
      console.error(`- ${error}`);
    }
    process.exit(1);
  }
}
