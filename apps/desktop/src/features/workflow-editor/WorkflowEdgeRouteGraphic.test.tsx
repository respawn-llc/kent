import { render, screen, within } from "@testing-library/react";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowEdgeRouteGraphic } from "./WorkflowEdgeRouteGraphic";

void initializeI18n();

describe("WorkflowEdgeRouteGraphic", () => {
  it("renders equal-width route labels with a fixed-size context-colored arrow", () => {
    render(
      <WorkflowEdgeRouteGraphic
        contextMode="compact_and_continue_session"
        sourceLabel="Backlog"
        targetLabel="Planning Preparation"
      />,
    );

    const graphic = screen.getByTestId("workflow-edge-route-graphic");
    const source = within(graphic).getByTestId("workflow-edge-route-source");
    const target = within(graphic).getByTestId("workflow-edge-route-target");
    const arrow = within(graphic).getByTestId("workflow-edge-route-arrow");

    expect(graphic).toHaveAccessibleName("Backlog to Planning Preparation");
    expect(graphic).toHaveClass("grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]");
    expect(source).toHaveClass("min-w-0", "place-items-center", "text-center");
    expect(target).toHaveClass("min-w-0", "place-items-center", "text-center");
    expect(source).not.toHaveClass("border", "bg-[var(--color-island-1)]");
    expect(target).not.toHaveClass("border", "bg-[var(--color-island-1)]");
    expect(within(source).getByText("Backlog")).toHaveClass("truncate");
    expect(within(target).getByText("Planning Preparation")).toHaveClass("truncate");
    expect(arrow).toHaveClass("size-5");
    expect(arrow).toHaveAttribute("width", "20");
    expect(arrow).toHaveAttribute("height", "20");
    expect(arrow).toHaveStyle({ color: "var(--color-secondary)" });
  });

  it("uses the workflow edge error color when validation marks the edge invalid", () => {
    render(
      <WorkflowEdgeRouteGraphic
        contextMode="new_session"
        hasError
        sourceLabel="Backlog"
        targetLabel="Planning Preparation"
      />,
    );

    expect(screen.getByTestId("workflow-edge-route-arrow")).toHaveStyle({ color: "var(--color-error)" });
  });
});
