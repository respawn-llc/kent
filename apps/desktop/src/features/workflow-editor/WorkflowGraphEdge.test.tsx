import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { describe, expect, it, vi } from "vitest";
import type * as XyflowReact from "@xyflow/react";

import { WorkflowGraphEdge } from "./WorkflowGraphEdge";

vi.mock("@xyflow/react", async (importOriginal) => {
  const actual = await importOriginal<typeof XyflowReact>();
  return {
    ...actual,
    EdgeLabelRenderer({ children }: Readonly<{ children?: ReactNode }>) {
      return createPortal(children, document.body);
    },
  };
});

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

  it("renders edge labels with the card island background and on-background text", () => {
    render(
      <svg>
        <WorkflowGraphEdge
          data={{
            contextMode: "compact_and_continue_session",
            entityID: "edge-1",
            entityKind: "edge",
            hasError: false,
            label: "Review",
            routePoints: [
              { x: 0, y: 0 },
              { x: 100, y: 0 },
            ],
            transitionGroupID: "tg-1",
          }}
          id="edge-1"
          onInspect={() => undefined}
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

    expect(screen.getByTestId("workflow-edge-label-edge-1")).toHaveClass(
      "island-surface-1",
      "text-[var(--color-on-background)]",
    );
    expect(screen.getByTestId("workflow-edge-label-edge-1")).not.toHaveClass(
      "text-[var(--color-on-island)]",
      "bg-[color-mix(in_srgb,var(--color-island-0)_94%,transparent)]",
      "bg-[var(--color-island-1)]",
    );
  });
});
