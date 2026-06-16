import { describe, expect, it } from "vitest";

import { shouldRefreshBoardFromProjectEvent } from "./useBoardData";

describe("shouldRefreshBoardFromProjectEvent", () => {
  it("refreshes active workflow task events", () => {
    expect(
      shouldRefreshBoardFromProjectEvent(
        eventParams({ resource: "task", workflow_id: "workflow-active" }),
        "workflow-route",
        "workflow-active",
      ),
    ).toBe(true);
  });

  it("skips unrelated workflow task events", () => {
    expect(
      shouldRefreshBoardFromProjectEvent(
        eventParams({ resource: "task", workflow_id: "workflow-other" }),
        "workflow-route",
        "workflow-active",
      ),
    ).toBe(false);
  });

  it("keeps workflow-link and unknown events conservative", () => {
    expect(
      shouldRefreshBoardFromProjectEvent(
        eventParams({ resource: "workflow_link", workflow_id: "workflow-other" }),
        "workflow-route",
        "workflow-active",
      ),
    ).toBe(true);
    expect(shouldRefreshBoardFromProjectEvent({}, "workflow-route", "workflow-active")).toBe(true);
  });
});

function eventParams(event: Readonly<Record<string, unknown>>) {
  return { event };
}
