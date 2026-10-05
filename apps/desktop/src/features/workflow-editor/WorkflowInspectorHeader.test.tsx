import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { isValidElement } from "react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { WorkflowInspectorHeader } from "./WorkflowInspectorHeader";

const headerAction = vi.hoisted(() => vi.fn<(action: unknown) => void>());
vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
  usePublishSidebarHeaderAction: headerAction,
}));

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
