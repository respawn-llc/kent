import { describe, expect, it, vi } from "bun:test";

import { autoLoadAvailable, directionalBoundary } from "./InfiniteListBoundary";

describe("InfiniteListBoundary directional state", () => {
  it("maps loading and failure states without owning error formatting", () => {
    const onRetry = vi.fn();
    expect(
      directionalBoundary({
        message: "formatted failure",
        failed: false,
        loading: true,
        onRetry,
        retryLabel: "Retry",
      }),
    ).toEqual({ state: "loading" });

    expect(
      directionalBoundary({
        message: "formatted failure",
        failed: true,
        loading: true,
        onRetry,
        retryLabel: "Retry",
      }),
    ).toEqual({ state: "loading" });

    const errorState = directionalBoundary({
      message: "formatted failure",
      failed: true,
      loading: false,
      onRetry,
      retryLabel: "Retry",
    });
    expect(errorState).toEqual({
      state: "error",
      message: "formatted failure",
      retryLabel: "Retry",
      onRetry,
    });
    expect(autoLoadAvailable(true, errorState)).toBe(false);
    expect(autoLoadAvailable(true, undefined)).toBe(true);
  });
});
