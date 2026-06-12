import { createHash } from "node:crypto";
import { readdir, readFile, stat } from "node:fs/promises";
import { basename, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import postcss from "postcss";

const appsRoot = fileURLToPath(new URL("..", import.meta.url));
const desktopRoot = join(appsRoot, "desktop");
const distRoot = join(desktopRoot, "dist");
const assetsRoot = join(distRoot, "assets");
const manifestPath = join(distRoot, ".vite", "manifest.json");
const noticePath = join(distRoot, "THIRD-PARTY-FONTS.txt");
const monaspaceSha256 =
  "6569968f448ae856ab5b57dff1f13b109b220ca8e3f664169e135fcb5c4f0721";

const expectedManifestAssets = [
  ...[
    "montserrat-cyrillic-ext-wght-normal.woff2",
    "montserrat-cyrillic-wght-normal.woff2",
    "montserrat-vietnamese-wght-normal.woff2",
    "montserrat-latin-ext-wght-normal.woff2",
    "montserrat-latin-wght-normal.woff2",
    "montserrat-cyrillic-ext-wght-italic.woff2",
    "montserrat-cyrillic-wght-italic.woff2",
    "montserrat-vietnamese-wght-italic.woff2",
    "montserrat-latin-ext-wght-italic.woff2",
    "montserrat-latin-wght-italic.woff2",
  ].map((fileName) => ({
    label: fileName,
    sourceSegments: [
      "node_modules",
      "@fontsource-variable",
      "montserrat",
      "files",
      fileName,
    ],
    outputStem: fileName.slice(0, -".woff2".length),
  })),
  {
    label: "Monaspace Neon variable webfont",
    sourceSegments: [
      "src",
      "assets",
      "fonts",
      "monaspace",
      "monaspace-neon-var-v1.400.woff2",
    ],
    outputStem: "monaspace-neon-var-v1.400",
    sha256: monaspaceSha256,
  },
];

const expectedFontFaces = [
  { family: "Builder Sans", style: "normal", weight: "100 900" },
  { family: "Builder Sans", style: "italic", weight: "100 900" },
  { family: "Builder Mono", style: "normal", weight: "200 800" },
];

const requiredNoticeText = [
  "@fontsource-variable/montserrat",
  "monaspace-webfont-variable-v1.400.zip",
  monaspaceSha256,
  "SIL OPEN FONT LICENSE Version 1.1",
  "Reserved Font Name",
];

const errors = await checkDesktopFontAssets();

if (errors.length > 0) {
  console.error("Desktop bundled font asset check failed:");
  for (const error of errors) {
    console.error(`- ${error}`);
  }
  process.exit(1);
}

export async function checkDesktopFontAssets() {
  const errors = [];
  const manifestEntries = await readManifestEntries(errors);
  await validateManifestAssets(manifestEntries, errors);
  await validateFontFaceRules(errors);
  await validateNotice(errors);
  return errors;
}

async function readManifestEntries(errors) {
  try {
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    return Object.entries(manifest).map(([key, value]) => ({
      key,
      file: value.file,
      src: value.src,
    }));
  } catch (error) {
    errors.push(
      `dist is missing readable Vite manifest: ${formatError(error)}.`,
    );
    return [];
  }
}

async function validateManifestAssets(manifestEntries, errors) {
  const fontAssetEntries = manifestEntries.filter((entry) =>
    entry.file.endsWith(".woff2"),
  );

  for (const expectedAsset of expectedManifestAssets) {
    const manifestEntry = fontAssetEntries.find((entry) =>
      matchesExpectedSource(entry, expectedAsset),
    );
    if (manifestEntry === undefined) {
      errors.push(`${expectedAsset.label} is missing from the Vite manifest.`);
      continue;
    }

    if (
      !manifestEntry.file.startsWith("assets/") ||
      !basename(manifestEntry.file).endsWith(".woff2")
    ) {
      errors.push(
        `${expectedAsset.label} emitted to unexpected path ${manifestEntry.file}.`,
      );
      continue;
    }

    if (
      !basename(manifestEntry.file).startsWith(`${expectedAsset.outputStem}-`)
    ) {
      errors.push(
        `${expectedAsset.label} emitted as unexpected file name ${manifestEntry.file}.`,
      );
    }

    const emittedPath = join(distRoot, manifestEntry.file);
    try {
      const fileStat = await stat(emittedPath);
      if (!fileStat.isFile()) {
        errors.push(`${displayPath(emittedPath)} is not a bundled file.`);
      }
    } catch {
      errors.push(`${displayPath(emittedPath)} is missing from dist.`);
      continue;
    }

    if (expectedAsset.sha256 !== undefined) {
      const actualSha256 = createHash("sha256")
        .update(await readFile(emittedPath))
        .digest("hex");
      if (actualSha256 !== expectedAsset.sha256) {
        errors.push(
          `${expectedAsset.label} SHA256 is ${actualSha256}, expected ${expectedAsset.sha256}.`,
        );
      }
    }
  }

  for (const manifestEntry of fontAssetEntries) {
    if (
      !expectedManifestAssets.some((expectedAsset) =>
        matchesExpectedSource(manifestEntry, expectedAsset),
      )
    ) {
      errors.push(`${manifestEntry.file} is an unexpected bundled font asset.`);
    }
  }
}

async function validateFontFaceRules(errors) {
  const cssPaths = await findFiles(assetsRoot, ".css");
  const fontFaces = [];

  for (const cssPath of cssPaths) {
    const root = postcss.parse(await readFile(cssPath, "utf8"), {
      from: cssPath,
    });
    root.walkAtRules("font-face", (rule) => {
      const declarations = new Map();
      rule.walkDecls((declaration) => {
        declarations.set(
          declaration.prop.toLowerCase(),
          declaration.value.trim(),
        );
      });
      fontFaces.push({
        family: normalizeCssString(declarations.get("font-family") ?? ""),
        style: declarations.get("font-style") ?? "",
        weight: declarations.get("font-weight") ?? "",
        source: declarations.get("src") ?? "",
        cssPath,
      });
    });
  }

  for (const expectedFontFace of expectedFontFaces) {
    const matchedFontFace = fontFaces.find(
      (fontFace) =>
        fontFace.family === expectedFontFace.family &&
        fontFace.style === expectedFontFace.style &&
        fontFace.weight === expectedFontFace.weight,
    );
    if (matchedFontFace === undefined) {
      errors.push(
        `built CSS does not declare ${expectedFontFace.family} ${expectedFontFace.style} ${expectedFontFace.weight}.`,
      );
    }
  }

  for (const fontFace of fontFaces) {
    if (fontFace.source.startsWith("local(")) {
      errors.push(
        `${displayPath(fontFace.cssPath)} uses local() for ${fontFace.family}.`,
      );
    }
  }
}

async function validateNotice(errors) {
  try {
    const notice = await readFile(noticePath, "utf8");
    for (const requiredText of requiredNoticeText) {
      if (!notice.includes(requiredText)) {
        errors.push(`font notice is missing ${requiredText}.`);
      }
    }
  } catch {
    errors.push("dist is missing THIRD-PARTY-FONTS.txt.");
  }
}

async function findFiles(root, extension) {
  const entries = await readdir(root, { withFileTypes: true });
  const paths = await Promise.all(
    entries.map(async (entry) => {
      const path = join(root, entry.name);
      if (entry.isDirectory()) {
        return findFiles(path, extension);
      }
      if (entry.isFile() && entry.name.endsWith(extension)) {
        return [path];
      }
      return [];
    }),
  );
  return paths.flat().sort();
}

function matchesExpectedSource(manifestEntry, expectedAsset) {
  return [manifestEntry.key, manifestEntry.src]
    .filter((path) => path !== undefined)
    .some((path) => endsWithPathSegments(path, expectedAsset.sourceSegments));
}

function endsWithPathSegments(path, expectedSegments) {
  const actualSegments = path.split("/");
  if (actualSegments.length < expectedSegments.length) {
    return false;
  }
  return expectedSegments.every(
    (expectedSegment, index) =>
      actualSegments[
        actualSegments.length - expectedSegments.length + index
      ] === expectedSegment,
  );
}

function normalizeCssString(value) {
  const trimmed = value.trim();
  if (trimmed.length >= 2) {
    const first = trimmed[0];
    const last = trimmed[trimmed.length - 1];
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1);
    }
  }
  return trimmed;
}

function formatError(error) {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function displayPath(path) {
  return relative(appsRoot, path);
}
