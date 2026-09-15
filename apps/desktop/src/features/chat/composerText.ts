export function mergeComposerText(
  current: string,
  incoming: string,
  direction: "append" | "prepend",
): string {
  if (current.length === 0) return incoming;
  if (incoming.length === 0) return current;
  return direction === "append" ? `${current}\n${incoming}` : `${incoming}\n${current}`;
}
