import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";
import "vitest";

// Remove when jest-dom ships its Vitest 5 adapter (upstream PR #742).
declare module "vitest" {
  interface Assertion<R extends void | Promise<void> = void, T = unknown> extends TestingLibraryMatchers<
    T,
    R
  > {}
  interface AsymmetricMatchersContaining extends TestingLibraryMatchers<unknown, unknown> {}
}
