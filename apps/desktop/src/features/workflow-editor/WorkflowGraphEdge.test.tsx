import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { WorkflowGraphEdge } from "./WorkflowGraphEdge";

describe("WorkflowGraphEdge", () => {
  it("inspects an edge from the visible path without bubbling to the React Flow wrapper", () => {
    const onInspect = vi.fn();
    const onWrapperClick = vi.fn();
    render(
      <svg onClick={onWrapperClick}>
        <WorkflowGraphEdge
          data={{
            contextMode: "compact_and_continue_session",
            entityID: "edge-1",
            entityKind: "edge",
            hasError: false,
            label: "",
            routePoints: [],
            transitionGroupID: "tg-1",
          }}
          id="edge-1"
          onInspect={onInspect}
          source="node-1"
          sourceX={0}
          sourceY={0}
          target="done"
          targetX={100}
          targetY={0}
          type="workflow"
        />
      </svg>,
    );

    fireEvent.click(screen.getByTestId("workflow-edge-path"));

    expect(onInspect).toHaveBeenCalledExactlyOnceWith("edge-1");
    expect(onWrapperClick).not.toHaveBeenCalled();
  });
});
