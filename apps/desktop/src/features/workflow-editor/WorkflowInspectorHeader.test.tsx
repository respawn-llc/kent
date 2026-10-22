import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { isValidElement } from "react";
import { expect, it, mock, vi } from "bun:test";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";

const headerAction = vi.fn<(action: unknown) => void>();
const actualAppFacade = await import("@/app-facade");
await mock.module("@/app-facade", () => ({
  ...actualAppFacade,
  usePublishSidebarHeaderAction: headerAction,
}));
const { WorkflowInspectorHeader } = await import("./WorkflowInspectorHeader");

it("copies Workflow Inspector Node and Transition IDs", async () => {
  const bridge = createBrowserNativeBridge();
  const services = createTestServices([], {
    ...bridge,
    capabilities: {
      ...bridge.capabilities,
      clipboard: { ...bridge.capabilities.clipboard, writeText: true },
    },
  });
  const copy = vi.spyOn(services.nativeBridge.clipboard, "writeText").mockResolvedValue();
  for (const selection of [
    { kind: "node", nodeID: "node-1" } as const,
    { kind: "edge", edgeID: "edge-1" } as const,
  ]) {
    render(
      <TestAppProviders services={services}>
        <WorkflowInspectorHeader selection={selection} workflowID="workflow-1" onDeleted={vi.fn()} />
      </TestAppProviders>,
    );
    const action = headerAction.mock.lastCall?.[0];
    if (!isValidElement(action)) throw new Error("Expected Workflow ID copy action.");
    render(<TestAppProviders services={services}>{action}</TestAppProviders>);
    const id = selection.kind === "node" ? selection.nodeID : selection.edgeID;
    fireEvent.click(screen.getByText(id));
    await waitFor(() => {
      expect(copy).toHaveBeenCalledWith(id);
    });
  }
});
