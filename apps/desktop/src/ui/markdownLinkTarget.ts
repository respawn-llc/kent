import { safeExternalUrl } from "./externalLinks";

export type MarkdownLinkTarget =
  Readonly<{ kind: "external"; url: string }> | Readonly<{ kind: "file"; path: string }>;

export function markdownLinkTarget(value: string | undefined): MarkdownLinkTarget | undefined {
  const external = safeExternalUrl(value);
  if (external !== undefined) return { kind: "external", url: external };
  if (value === undefined || value.startsWith("//") || invalidPathCharacters(value)) return undefined;

  try {
    const absolutePath = value.startsWith("/");
    const url = absolutePath ? new URL(value, "file:///") : new URL(value);
    if (!absolutePath && !value.slice(url.protocol.length).startsWith("//")) {
      return undefined;
    }
    const path = localFilePath(url);
    return path === undefined ? undefined : { kind: "file", path };
  } catch {
    return undefined;
  }
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
