export function contextPresentation(used: number, window: number | null) {
  if (window === null || window <= 0) return null;
  const ratio = used / window;
  return {
    usedPercent: Math.round(ratio * 100),
    used: formatTokens(used),
    window: formatTokens(window),
    extent: Math.min(1, Math.max(0, ratio)),
  };
}

function formatTokens(tokens: number): string {
  return Math.abs(tokens) < 1000 ? tokens.toString() : `${(tokens / 1000).toFixed(1)}k`;
}
