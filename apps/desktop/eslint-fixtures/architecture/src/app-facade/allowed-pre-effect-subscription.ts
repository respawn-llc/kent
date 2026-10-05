export function subscribe(listener: () => void) {
  listener();
  return () => undefined;
}
