import * as Stream from "effect/Stream";

const listeners = new Set<(value: StorageEvent) => void>();
export const events = Stream.empty;
export function deliver(value: StorageEvent): void {
  for (const listener of listeners) listener(value);
}
