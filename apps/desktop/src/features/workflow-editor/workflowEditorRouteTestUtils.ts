// Shared non-component test utilities for the split WorkflowEditorRoute tests:
// route builders, DOM/layout mocks, native bridge stubs, and assertion helpers.
import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeDialogWindowOptions,
  type NativeWorkflowDeleted,
  type NativeWorkflowGraphDeleteConfirmation,
} from "@app/native-bridge";
import { fireEvent, within } from "@testing-library/react";

import { protocolVersion } from "../../api/jsonRpcSocket";
import { type createTestServices, startupRoutes } from "../../testSupport/appServices";
import { graphValidationResponse, invalidValidationResponse } from "./workflowEditorRouteValidationFixtures";
import { workflowDefinitionResponse } from "./workflowEditorRouteWorkflowFixtures";

export class MockResizeObserver implements ResizeObserver {
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

export function workflowEditorRoutes() {
  return [
    ...startupRoutes,
    { method: "workflow.get", result: workflowDefinitionResponse },
    { method: "workflow.validate", result: invalidValidationResponse },
    { method: "workflow.graph.validateDraft", result: graphValidationResponse },
  ];
}

export function startupRoutesWithSubagentRoles(roleNames: readonly string[]) {
  return [
    {
      method: "server.readiness.get",
      result: {
        auth_ready: true,
        auth_required: true,
        endpoint: "ws://127.0.0.1:53082/rpc",
        protocol_version: protocolVersion,
        ready: true,
        server_build: "1.3.0",
        server_id: "server-1",
        server_version: "1.3.0",
        subagent_roles: roleNames.map((name) => ({ name })),
      },
    },
    ...startupRoutes.filter((route) => route.method !== "server.readiness.get"),
  ];
}

export function mockWindowWidth(width: number): () => void {
  const descriptor = Object.getOwnPropertyDescriptor(window, "innerWidth");
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  return () => {
    if (descriptor === undefined) {
      Reflect.deleteProperty(window, "innerWidth");
      return;
    }
    Object.defineProperty(window, "innerWidth", descriptor);
  };
}

export function parameterKeys(container: HTMLElement): readonly string[] {
  return within(container)
    .getAllByTestId("workflow-parameter")
    .map((field) => field.dataset.parameterKey ?? "");
}

export function workflowDraftValidationCallCount(services: ReturnType<typeof createTestServices>): number {
  return services.transport.calls.filter((call) => call.method === "workflow.graph.validateDraft").length;
}

export function workflowGraphSaveEdges(params: unknown): readonly Readonly<Record<string, unknown>>[] {
  if (!isRecord(params) || !isRecord(params.graph) || !Array.isArray(params.graph.edges)) {
    throw new Error("Expected workflow graph save params with graph edges.");
  }
  if (!params.graph.edges.every(isRecord)) {
    throw new Error("Expected workflow graph save edges to be objects.");
  }
  return params.graph.edges;
}

export function hasRepairedGroupedBranchFanout(params: unknown): boolean {
  if (!isRecord(params) || !isRecord(params.graph) || !Array.isArray(params.graph.edges)) {
    return false;
  }
  const branchTransitionGroupIDs = params.graph.edges
    .filter(isRecord)
    .filter((edge) => edge.target_node_id === "impl-a" || edge.target_node_id === "impl-b")
    .map((edge) => edge.transition_group_id);
  return (
    branchTransitionGroupIDs.length === 2 &&
    branchTransitionGroupIDs.every((transitionGroupID) => transitionGroupID === "tg-plan-new-agent")
  );
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}

export function dispatchMouseEvent(
  target: Document | Element | Window,
  view: Window & typeof globalThis,
  type: "mousedown" | "mousemove" | "mouseup",
  options: MouseEventInit,
): void {
  const event = new view.MouseEvent(type, { bubbles: true, cancelable: true, ...options });
  Object.defineProperty(event, "view", { value: view });
  fireEvent(target, event);
}

export function dragWorkflowNodeOut(card: HTMLElement): void {
  const eventView = card.ownerDocument.defaultView;
  if (eventView === null) {
    throw new Error("Expected test document to have a default window");
  }
  const restoreElementFromPoint = mockDocumentElementFromPoint(
    vi.fn<typeof document.elementFromPoint>(() => card),
  );
  try {
    dispatchMouseEvent(card, eventView, "mousedown", { button: 0, clientX: 12, clientY: 18 });
    dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 28, clientY: 34 });
    dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 500, clientY: 500 });
    dispatchMouseEvent(document, eventView, "mouseup", { clientX: 500, clientY: 500 });
  } finally {
    restoreElementFromPoint();
  }
}

export function mockDocumentElementFromPoint(elementFromPoint: typeof document.elementFromPoint): () => void {
  const originalElementFromPoint = Object.getOwnPropertyDescriptor(document, "elementFromPoint");
  const fallbackElementFromPoint: typeof document.elementFromPoint = () => null;
  Object.defineProperty(document, "elementFromPoint", {
    configurable: true,
    value: elementFromPoint,
  });
  return () => {
    if (originalElementFromPoint === undefined) {
      Object.defineProperty(document, "elementFromPoint", {
        configurable: true,
        value: fallbackElementFromPoint,
      });
      return;
    }
    Object.defineProperty(document, "elementFromPoint", originalElementFromPoint);
  };
}

export function expectIdentifierInputCorrectionsDisabled(input: HTMLElement): void {
  expect(input).toHaveAttribute("autocapitalize", "none");
  expect(input).toHaveAttribute("autocomplete", "off");
  expect(input).toHaveAttribute("autocorrect", "off");
  expect(input).toHaveAttribute("spellcheck", "false");
}

export function mockSidebarLayout(shellWidth: () => number): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this.dataset.testid === "app-shell-content") {
      return domRect({ height: 720, width: shellWidth() });
    }
    return domRect({ height: 720, width: 560 });
  });
}

export function mockParameterLayout(): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this.dataset.testid === "workflow-parameter") {
      return domRect({ height: 96, top: this.dataset.parameterKey === "summary" ? 140 : 20, width: 320 });
    }
    return domRect({ height: 96, top: 20, width: 320 });
  });
}

function domRect({
  height,
  top = 0,
  width,
}: Readonly<{ height: number; top?: number; width: number }>): DOMRect {
  return {
    bottom: top + height,
    height,
    left: 0,
    right: width,
    top,
    width,
    x: 0,
    y: top,
    toJSON: () => ({}),
  };
}

export function sidebarWidthStyle(sidebar: HTMLElement): string {
  return sidebar.style.getPropertyValue("--app-sidebar-width");
}

export function nativeBridgeWithClipboard(copied: string[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      clipboard: { ...base.capabilities.clipboard, writeText: true },
    },
    clipboard: {
      ...base.clipboard,
      async writeText(value): Promise<void> {
        copied.push(value);
      },
    },
  };
}

export function nativeWorkflowDeleteDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
  const handlers = new Set<(confirmation: NativeWorkflowGraphDeleteConfirmation) => void>();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
    dialogs: {
      ...base.dialogs,
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
    workflowEditor: {
      async confirmGraphDelete(confirmation): Promise<void> {
        for (const handler of handlers) {
          handler(confirmation);
        }
      },
      async onGraphDeleteConfirmed(handler): Promise<() => void> {
        handlers.add(handler);
        return () => {
          handlers.delete(handler);
        };
      },
    },
  };
}

export function nativeWorkflowEntityDeleteDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
  const handlers = new Set<(event: NativeWorkflowDeleted) => void>();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
    dialogs: {
      ...base.dialogs,
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
    workflowDeletion: {
      async notifyDeleted(event): Promise<void> {
        for (const handler of handlers) {
          handler(event);
        }
      },
      async onDeleted(handler): Promise<() => void> {
        handlers.add(handler);
        return () => {
          handlers.delete(handler);
        };
      },
    },
  };
}

export function nativeWorkflowDeleteWindowBridge(
  onClose: () => void,
  onDeleted: (event: NativeWorkflowDeleted) => void,
): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async closeCurrent(): Promise<void> {
        onClose();
      },
    },
    workflowDeletion: {
      ...base.workflowDeletion,
      async notifyDeleted(event): Promise<void> {
        onDeleted(event);
      },
    },
  };
}

export function nativeWorkflowDeleteListenerFailureBridge(): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    workflowEditor: {
      ...base.workflowEditor,
      async onGraphDeleteConfirmed(): Promise<() => void> {
        throw new Error("listener unavailable");
      },
    },
  };
}
