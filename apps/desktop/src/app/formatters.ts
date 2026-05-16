const relativeFormatter = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

export function formatRelativeTime(unixMs: number, nowMs = Date.now()): string {
  if (unixMs <= 0) {
    return "";
  }
  const deltaSeconds = Math.round((unixMs - nowMs) / 1000);
  const minutes = Math.round(deltaSeconds / 60);
  const hours = Math.round(minutes / 60);
  const days = Math.round(hours / 24);
  if (Math.abs(days) >= 1) {
    return relativeFormatter.format(days, "day");
  }
  if (Math.abs(hours) >= 1) {
    return relativeFormatter.format(hours, "hour");
  }
  if (Math.abs(minutes) >= 1) {
    return relativeFormatter.format(minutes, "minute");
  }
  return relativeFormatter.format(deltaSeconds, "second");
}

export function basename(path: string): string {
  const normalized = path.trim().replaceAll("\\", "/");
  const parts = normalized.split("/").filter((part) => part.length > 0);
  return parts.at(-1) ?? normalized;
}

export function projectKeyFromName(name: string): string {
  let letters = "";
  for (const char of name.toUpperCase()) {
    if (isProjectKeyChar(char)) {
      letters += char;
    }
  }
  const candidate = letters.length >= 2 ? letters : `${letters}PR`;
  return candidate.slice(0, 8);
}

function isProjectKeyChar(char: string): boolean {
  const code = char.charCodeAt(0);
  return (code >= 65 && code <= 90) || (code >= 48 && code <= 57);
}
