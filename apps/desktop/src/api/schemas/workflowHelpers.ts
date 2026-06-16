export function emptyArray<T>(value: T[] | null | undefined): T[] {
  return value ?? [];
}

export function firstNonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.length > 0) ?? "";
}
