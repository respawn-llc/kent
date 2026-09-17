export function contextPresentation(used: number, window: number | null) {
  if (window === null || window <= 0) return null;
  const ratio = used / window;
  const remaining = window - used;
  return {
    usedPercent: Math.round(ratio * 100),
    remainingPercent: Math.round((remaining / window) * 100),
    remaining: formatTokens(remaining),
    window: formatTokens(window),
    extent: Math.min(1, Math.max(0, ratio)),
  };
}

function formatTokens(tokens: number): string {
  return Math.abs(tokens) < 1000 ? tokens.toString() : `${Math.trunc(tokens / 1000).toString()}k`;
}
