import { CancelledError } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, mock, vi } from "bun:test";
import type * as ReactI18next from "react-i18next";

const push = vi.fn();

const actualAppFacade = await import("@/app-facade");
const actualReactI18next: typeof ReactI18next = await import("react-i18next");
await mock.module("@/app-facade", () => ({
  ...actualAppFacade,
  useStatusController: () => ({ push }),
}));

await mock.module("react-i18next", () => ({
  ...actualReactI18next,
  useTranslation: () => ({ t: (key: string) => key }),
}));

const { useBoardLoadErrorReporter } = await import("./useBoardLoadErrorReporter");

describe("useBoardLoadErrorReporter", () => {
  beforeEach(() => {
    push.mockReset();
  });

  it("reports refresh failures", () => {
    const { result } = renderHook(useBoardLoadErrorReporter);
    const failure = new Error("refresh failed");

    act(() => {
      result.current(failure);
    });

    expect(push).toHaveBeenCalledWith(
      expect.objectContaining({
        body: "refresh failed",
        tone: "danger",
      }),
    );
  });

  it("does not surface query cancellation", () => {
    const { result } = renderHook(useBoardLoadErrorReporter);

    act(() => {
      result.current(new CancelledError());
    });

    expect(push).not.toHaveBeenCalled();
  });
});
