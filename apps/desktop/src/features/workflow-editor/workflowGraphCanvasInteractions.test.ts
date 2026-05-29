import { afterEach, describe, expect, it, vi } from "vitest";

import { connectWorkflowGraphNodes, groupIDFromPoint, inspectNode } from "./workflowGraphCanvasInteractions";

describe("workflowGraphCanvasInteractions", () => {
  afterEach(() => {
    document.body.replaceChildren();
    vi.restoreAllMocks();
  });

  it("falls back to group bounds when a dragged card covers the pointer target", () => {
    const draggedCard = document.createElement("div");
    document.body.append(groupElement("group-a", rect(10, 20, 200, 160)), draggedCard);
    installElementFromPoint(draggedCard);

    expect(groupIDFromPoint(80, 90)).toBe("group-a");
  });

  it("chooses the smallest containing group when fallback bounds overlap", () => {
    const draggedCard = document.createElement("div");
    document.body.append(
      groupElement("outer-group", rect(0, 0, 300, 300)),
      groupElement("inner-group", rect(50, 50, 100, 100)),
      draggedCard,
    );
    installElementFromPoint(draggedCard);

    expect(groupIDFromPoint(75, 75)).toBe("inner-group");
    expect(groupIDFromPoint(25, 25)).toBe("outer-group");
  });

  it("forwards React Flow handle-drag connections to the workflow connection callback", () => {
    const onConnectNodes = vi.fn();

    connectWorkflowGraphNodes({ source: "agent", target: "join" }, onConnectNodes);

    expect(onConnectNodes).toHaveBeenCalledWith("agent", "join");
  });

  it("opens inspectors for editable workflow node kinds", () => {
    const onGroupInspect = vi.fn();
    const onNodeInspect = vi.fn();

    for (const kind of ["start", "agent", "join", "terminal"]) {
      inspectNode(
        {
          data: { entityID: `node-${kind}`, entityKind: "node", kind },
          id: `node-${kind}`,
          position: { x: 0, y: 0 },
        },
        onGroupInspect,
        onNodeInspect,
      );
    }

    expect(onNodeInspect).toHaveBeenNthCalledWith(1, "node-start");
    expect(onNodeInspect).toHaveBeenNthCalledWith(2, "node-agent");
    expect(onNodeInspect).toHaveBeenNthCalledWith(3, "node-join");
    expect(onNodeInspect).toHaveBeenNthCalledWith(4, "node-terminal");
    expect(onGroupInspect).not.toHaveBeenCalled();
  });
});

function groupElement(groupID: string, bounds: DOMRect): HTMLElement {
  const element = document.createElement("section");
  element.dataset.workflowGroupId = groupID;
  Object.defineProperty(element, "getBoundingClientRect", {
    configurable: true,
    value: () => bounds,
  });
  return element;
}

function installElementFromPoint(element: Element): void {
  Object.defineProperty(document, "elementFromPoint", {
    configurable: true,
    value: vi.fn<typeof document.elementFromPoint>(() => element),
  });
}

function rect(x: number, y: number, width: number, height: number): DOMRect {
  const view = document.defaultView;
  if (view === null) {
    throw new Error("Expected test document to have a default window");
  }
  return new view.DOMRect(x, y, width, height);
}
