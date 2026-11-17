import { safeExternalUrl } from "./externalLinks";

export type MarkdownLinkTarget =
  Readonly<{ kind: "external"; url: string }> | Readonly<{ kind: "file"; path: string }>;

export function markdownLinkTarget(
  value: string | undefined,
  allowRelative = false,
): MarkdownLinkTarget | undefined {
  const external = safeExternalUrl(value);
  if (external !== undefined) return { kind: "external", url: external };
  if (value === undefined || value.startsWith("//") || invalidPathCharacters(value)) return undefined;

  try {
    const absolutePath = value.startsWith("/");
    if (allowRelative && !absolutePath && !URL.canParse(value) && value !== "") {
      return relativeFileTarget(value);
    }
    return absoluteFileTarget(value, absolutePath);
  } catch {
    return undefined;
  }
}

function relativeFileTarget(value: string): MarkdownLinkTarget | undefined {
  const uri = new URL(value, "file:///");
  const path = decodeURIComponent(value);
  return uri.search === "" && uri.hash === "" && !invalidPathCharacters(path)
    ? { kind: "file", path }
    : undefined;
}

function absoluteFileTarget(value: string, absolutePath: boolean): MarkdownLinkTarget | undefined {
  const url = absolutePath ? new URL(value, "file:///") : new URL(value);
  if (!absolutePath && !value.slice(url.protocol.length).startsWith("//")) return undefined;
  const path = localFilePath(url);
  return path === undefined ? undefined : { kind: "file", path };
}

function localFilePath(url: URL): string | undefined {
  if (url.protocol !== "file:" || url.hostname !== "" || url.search !== "" || url.hash !== "") {
    return undefined;
  }
  const path = decodeURIComponent(url.pathname);
  return !path.startsWith("/") || path.startsWith("//") || invalidPathCharacters(path) ? undefined : path;
}

function invalidPathCharacters(value: string): boolean {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code < 32 || code === 127 || character === "\\") return true;
  }
  return false;
}
