const maxModelKeyLength = 64;

export function workflowModelKeyFromLabel(label: string, fallback: string): string {
  const normalized = normalizeModelKey(label);
  const fallbackKey = normalizeModelKey(fallback);
  const base = normalized.length > 0 ? normalized : fallbackKey;
  return ensureLeadingLetter(base.length > 0 ? base : "item");
}

export function uniqueWorkflowModelKey(
  preferred: string,
  existing: ReadonlySet<string> | readonly string[],
): string {
  const existingKeys = existing instanceof Set ? existing : new Set(existing);
  const base = workflowModelKeyFromLabel(preferred, "item");
  if (!existingKeys.has(base)) {
    return base;
  }
  for (let index = 2; index < Number.MAX_SAFE_INTEGER; index += 1) {
    const suffix = `_${index.toString()}`;
    const candidate = `${base.slice(0, maxModelKeyLength - suffix.length)}${suffix}`;
    if (!existingKeys.has(candidate)) {
      return candidate;
    }
  }
  throw new Error("unable to create unique workflow model key");
}

function normalizeModelKey(value: string): string {
  const out: string[] = [];
  let previousUnderscore = false;
  for (const char of value.trim().toLowerCase()) {
    const normalized = modelKeyChar(char);
    if (normalized === "_") {
      if (out.length > 0 && !previousUnderscore) {
        out.push(normalized);
        previousUnderscore = true;
      }
      continue;
    }
    out.push(normalized);
    previousUnderscore = false;
    if (out.length >= maxModelKeyLength) {
      break;
    }
  }
  return trimTrailingUnderscore(out.join(""));
}

function modelKeyChar(char: string): string {
  return isLowerAsciiLetter(char) || isAsciiDigit(char) ? char : "_";
}

function ensureLeadingLetter(value: string): string {
  if (isLowerAsciiLetter(value[0] ?? "")) {
    return value;
  }
  return `x_${value}`.slice(0, maxModelKeyLength);
}

function trimTrailingUnderscore(value: string): string {
  let end = value.length;
  while (end > 0 && value[end - 1] === "_") {
    end -= 1;
  }
  return value.slice(0, end);
}

function isLowerAsciiLetter(char: string): boolean {
  return char >= "a" && char <= "z";
}

function isAsciiDigit(char: string): boolean {
  return char >= "0" && char <= "9";
}
