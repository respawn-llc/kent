import * as Stream from "effect/Stream";

export const events = Stream.empty;
export const listeners = new Set<(value: string) => void>();
export function subscribe(listener: (value: string) => void): () => void {
  listener("value");
  return () => undefined;
}
