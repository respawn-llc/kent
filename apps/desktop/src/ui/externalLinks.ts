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

/**
 * Produces a compact display label for an external URL, preferring the bare
 * host (without a leading `www.`) so links read as e.g. `github.com`. Falls
 * back to the full URL when no host is present (e.g. `mailto:` addresses).
 */
export function compactExternalUrlLabel(url: string): string {
  try {
    const { hostname } = new URL(url);
    if (hostname.length === 0) {
      return url;
    }
    return hostname.startsWith("www.") ? hostname.slice("www.".length) : hostname;
  } catch {
    return url;
  }
}
