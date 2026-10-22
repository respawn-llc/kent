import type { expect, MatchersBuiltin } from "bun:test";
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

export {};

type BrowserMatchers<T> = Omit<
  TestingLibraryMatchers<ReturnType<typeof expect.stringContaining>, T>,
  keyof MatchersBuiltin<T>
>;

declare module "bun:test" {
  interface Matchers<T = unknown> extends BrowserMatchers<T> {}
}
