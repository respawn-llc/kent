const safeProtocols = new Set(["http:", "https:", "mailto:"]);

/**
 * Validates a candidate external URL, returning a normalized absolute URL when
 * it parses and uses an allowed protocol, or `undefined` otherwise. Use this to
 * decide whether a stored value should be rendered as a clickable link.
 */
export function safeExternalUrl(value: string | undefined): string | undefined {
  if (value === undefined || value.trim().length === 0) {
    return undefined;
  }

  try {
    const parsed = new URL(value);
    return safeProtocols.has(parsed.protocol) ? parsed.toString() : undefined;
  } catch {
    return undefined;
  }
}

/** Formats recognized issue/page URLs without fetching remote metadata. */
export function compactExternalUrlLabel(url: string): string {
  try {
    const parsed = new URL(url);
    const { hostname } = parsed;
    if (hostname.length === 0) {
      return url;
    }
    return sourceIdentity(parsed) ?? (hostname.startsWith("www.") ? hostname.slice("www.".length) : hostname);
  } catch {
    return url;
  }
}

function sourceIdentity(url: URL): string | undefined {
  const rawParts = url.pathname.split("/").slice(1);
  try {
    const parts = rawParts.map(decodeURIComponent);
    return (
      githubIdentity(url, parts) ??
      jiraIdentity(url, parts) ??
      linearIdentity(url, parts) ??
      confluenceIdentity(url, rawParts) ??
      gitlabIdentity(parts) ??
      azureIdentity(url, parts) ??
      youtrackIdentity(url, parts)
    );
  } catch {
    return undefined;
  }
}

function githubIdentity(url: URL, parts: readonly string[]): string | undefined {
  const [owner, repo, kind, id] = parts;
  if (
    url.hostname !== "github.com" ||
    parts.length !== 4 ||
    !owner ||
    !repo ||
    kind !== "issues" ||
    !positiveID(id)
  )
    return undefined;
  return `${owner}/${repo}#${id}`;
}

function atlassianSite(url: URL): string {
  return url.hostname.endsWith(".atlassian.net")
    ? url.hostname.slice(0, -".atlassian.net".length)
    : url.hostname;
}

function jiraIdentity(url: URL, parts: readonly string[]): string | undefined {
  const key = parts.at(-1);
  if (parts.at(-2) !== "browse" || !issueKey(key)) return undefined;
  return `${atlassianSite(url)} · ${key}`;
}

function linearIdentity(url: URL, parts: readonly string[]): string | undefined {
  const [workspace, kind, key] = parts;
  if (url.hostname !== "linear.app" || !workspace || kind !== "issue" || !issueKey(key)) return undefined;
  return `${workspace} · ${key}`;
}

function confluenceIdentity(url: URL, parts: readonly string[]): string | undefined {
  const path = parts[0] === "wiki" || parts[0] === "confluence" ? parts.slice(1) : parts;
  const page = confluencePathTitle(path) ?? confluenceQueryTitle(url, path);
  return page === undefined ? undefined : `${atlassianSite(url)} · ${page}`;
}

function confluencePathTitle(parts: readonly string[]): string | undefined {
  const [kind, space, pages, id, title] = parts;
  if (!space) return undefined;
  if (kind === "display" && parts.length === 3 && pages)
    return decodeURIComponent(pages.split("+").join(" "));
  if (kind !== "spaces" || pages !== "pages" || !positiveID(id) || parts.length > 5) return undefined;
  return title ? decodeURIComponent(title.split("+").join(" ")) : `Page ${id}`;
}

function confluenceQueryTitle(url: URL, parts: readonly string[]): string | undefined {
  if (parts.length !== 2 || parts[0] !== "pages" || parts[1] !== "viewpage.action") return undefined;
  const id = url.searchParams.get("pageId");
  return id !== null && positiveID(id) ? `Page ${id}` : undefined;
}

function gitlabIdentity(parts: readonly string[]): string | undefined {
  const id = parts.at(-1);
  if (parts.length < 5 || parts.at(-3) !== "-" || parts.at(-2) !== "issues" || !positiveID(id))
    return undefined;
  const repo = parts.slice(0, -3);
  return repo.every((part) => part.length > 0) ? `${repo.join("/")}#${id}` : undefined;
}

function azureIdentity(url: URL, parts: readonly string[]): string | undefined {
  const path = url.hostname === "dev.azure.com" ? parts.slice(1) : parts;
  if (url.hostname !== "dev.azure.com" && !url.hostname.endsWith(".visualstudio.com")) return undefined;
  const [project, kind, action, id] = path;
  if (path.length !== 4 || !project || kind !== "_workitems" || action !== "edit" || !positiveID(id))
    return undefined;
  return `${project}#${id}`;
}

function youtrackIdentity(url: URL, parts: readonly string[]): string | undefined {
  const [kind, key] = parts;
  if (kind !== "issue" || !issueKey(key) || parts.length < 2 || parts.length > 3) return undefined;
  const site = url.hostname.endsWith(".youtrack.cloud")
    ? url.hostname.slice(0, -".youtrack.cloud".length)
    : url.hostname;
  return `${site} · ${key}`;
}

function positiveID(value: string | undefined): value is string {
  if (value === undefined || value.length === 0 || !Number.isSafeInteger(Number(value)) || Number(value) <= 0)
    return false;
  for (const character of value) {
    if (character < "0" || character > "9") return false;
  }
  return true;
}

function issueKey(value: string | undefined): value is string {
  if (value === undefined) return false;
  const separator = value.lastIndexOf("-");
  const key = value.slice(0, separator);
  if (separator <= 0 || !positiveID(value.slice(separator + 1))) return false;
  for (const character of key) {
    if ((character < "A" || character > "Z") && (character < "0" || character > "9") && character !== "_")
      return false;
  }
  return true;
}
