import { nextReachableHistoryIndex } from "./navigation";

describe("navigation stack state", () => {
  it("preserves forward availability after back and truncates it on push", () => {
    const afterPushes = nextReachableHistoryIndex(nextReachableHistoryIndex(0, "PUSH", 1), "PUSH", 2);
    const afterBack = nextReachableHistoryIndex(afterPushes, "BACK", 1);
    const canGoForwardAfterBack = 1 < afterBack;
    const afterPushFromBack = nextReachableHistoryIndex(afterBack, "PUSH", 2);
    const canGoForwardAfterPush = 2 < afterPushFromBack;

    expect(canGoForwardAfterBack).toBe(true);
    expect(canGoForwardAfterPush).toBe(false);
  });
});
