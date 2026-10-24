export function presentUsageWindow(
  window: Readonly<{
    usedPercent: number;
    contributions: Readonly<{ build: number | null; chat: number | null }> | null;
  }>,
  source: "chatgpt" | "grok" | null,
) {
  const used = clamp(window.usedPercent);
  const percent = source === "grok" ? used : 100 - used;
  const supplied = window.contributions;
  const contributions =
    source === "grok" &&
    supplied?.build != null &&
    supplied.chat !== null &&
    Math.abs(clamp(supplied.build) + clamp(supplied.chat) - used) <= 0.01
      ? { build: clamp(supplied.build), chat: clamp(supplied.chat) }
      : null;
  return {
    percent,
    kind: source === "grok" ? ("consumed" as const) : ("remaining" as const),
    segments:
      contributions === null
        ? [{ id: source === "grok" ? "consumed" : "remaining", percent }]
        : [
            { id: "build", percent: contributions.build },
            { id: "chat", percent: contributions.chat },
          ],
    contributions,
  };
}

function clamp(value: number) {
  return Math.max(0, Math.min(100, value));
}
