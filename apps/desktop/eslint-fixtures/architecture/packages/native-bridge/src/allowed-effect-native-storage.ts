import * as Stream from "effect/Stream";

const listeners = new Set<(value: string) => void>();
export const events = Stream.empty;
export function deliver(value: string): void {
  for (const listener of listeners) listener(value);
}
