import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, vi } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";

void initializeI18n();

describe("WorkflowGraphToolbar", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
  });

  it("shows explicit hover tooltips for each toolbar action", async () => {
    const user = userEvent.setup();
    render(
      <WorkflowGraphCanvas
        graph={{ edges: [], nodes: [] }}
        onAddNode={() => undefined}
        onEdgeInspect={() => undefined}
        onGroupInspect={() => undefined}
        onNodeInspect={() => undefined}
        onWorkflowInspect={() => undefined}
      />,
    );

    const toolbar = within(screen.getByTestId("workflow-editor-tools"));
    expect(
      toolbar.getAllByRole("button").map((button) => [
        button.getAttribute("aria-label"),
        button.getAttribute("title"),
      ]),
    ).toEqual([
      ["Add node", "Add node"],
      ["Inspect workflow", "Edit workflow"],
      ["Actual size", "Actual size"],
      ["Zoom in", "Zoom +"],
      ["Zoom out", "Zoom -"],
      ["Fit view", "Fit view"],
    ]);

    const addNode = toolbar.getByRole("button", { name: "Add node" });
    fireEvent.click(addNode);
    expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();

    await user.hover(toolbar.getByRole("button", { name: "Zoom in" }));
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Zoom +");
    expect(screen.queryByTestId("tooltip-arrow")).not.toBeInTheDocument();

    fireEvent.pointerEnter(addNode);
    expect(await screen.findByRole("button", { name: "Agent node" })).toBeInTheDocument();
    fireEvent.pointerLeave(addNode);
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();
    });
    await new Promise((resolve) => setTimeout(resolve, 180));
    expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();

    fireEvent.pointerEnter(addNode);
    expect(await screen.findByRole("button", { name: "Agent node" })).toBeInTheDocument();
    const addNodeMenu = screen.getByRole("dialog");
    fireEvent.pointerLeave(addNode);
    fireEvent.pointerEnter(addNodeMenu);
    await new Promise((resolve) => setTimeout(resolve, 180));
    expect(screen.getByRole("button", { name: "Agent node" })).toBeInTheDocument();
    fireEvent.pointerLeave(addNodeMenu);
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();
    });
    await new Promise((resolve) => setTimeout(resolve, 180));
    expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();

    fireEvent.focus(addNode);
    expect(await screen.findByRole("button", { name: "Agent node" })).toBeInTheDocument();
    fireEvent.blur(addNode, { relatedTarget: document.body });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Agent node" })).not.toBeInTheDocument();
    });
  });
});

class MockResizeObserver implements ResizeObserver {
  observe(): void {
    return;
  }

  unobserve(): void {
    return;
  }

  disconnect(): void {
    return;
  }
}
