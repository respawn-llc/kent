export function contextPresentation(used: number, window: number | null) {
  if (window === null || window <= 0) return null;
  const ratio = used / window;
  const remaining = window - used;
  return {
    usedPercent: Math.round(ratio * 100),
    remainingPercent: Math.round((remaining / window) * 100),
    remaining:
      Math.abs(remaining) < 1000 ? remaining.toString() : `${Math.trunc(remaining / 1000).toString()}k`,
    extent: Math.min(1, Math.max(0, ratio)),
  };
}
