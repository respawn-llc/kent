import {
  createBrowserNativeBridge,
  createTauriNativeBridge,
  type NativeBridge,
  type NativeDialogWindowOptions,
} from "@app/native-bridge";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import type { JsonValue } from "../../api/json";

export function installBoardRouteLifecycle(): void {
  const originalUserAgent = window.navigator.userAgent;

  beforeEach(() => {
    installStorage("localStorage");
    installStorage("sessionStorage");
    setNavigatorUserAgent(originalUserAgent);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    setNavigatorUserAgent(originalUserAgent);
  });
}

export async function expandBoardHoverMenu(): Promise<HTMLElement> {
  const menu = await screen.findByRole("navigation");
  fireEvent.mouseEnter(menu);
  return menu;
}

type BoardColumnFixtureInput = Readonly<{
  nodeID: string;
  key: string;
  kind: string;
  displayName: string;
  sortOrder: number;
  assigneeRole?: string;
  groupID?: string;
  isBacklog?: boolean;
  isDone?: boolean;
  taskCount?: number;
}>;

export function boardColumn(input: BoardColumnFixtureInput) {
  return {
    node: {
      node_id: input.nodeID,
      key: input.key,
      kind: input.kind,
      display_name: input.displayName,
      assignee_role: input.assigneeRole ?? "",
    },
    group_id: input.groupID ?? "",
    sort_order: input.sortOrder,
    is_backlog: input.isBacklog ?? false,
    is_done: input.isDone ?? false,
    task_count: input.taskCount ?? 0,
  };
}

export class TestDataTransfer {
  readonly #values = new Map<string, string>();
  dropEffect = "none";
  effectAllowed = "all";

  get types(): readonly string[] {
    return [...this.#values.keys()];
  }

  setData(type: string, value: string): void {
    this.#values.set(type, value);
  }

  getData(type: string): string {
    return this.#values.get(type) ?? "";
  }
}

export class DroppedPayloadDataTransfer {
  dropEffect = "none";
  effectAllowed = "all";
  readonly types: readonly string[] = [];

  setData(): void {
    // Browser shells may omit custom drag payloads on drop; board route must not rely on them.
  }

  getData(): string {
    return "";
  }
}

export function isObject(value: JsonValue): value is Readonly<Record<string, JsonValue>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function setScrollMetrics(
  element: HTMLElement,
  metrics: Readonly<{ clientHeight: number; scrollHeight: number; scrollTop: number }>,
): void {
  Object.defineProperty(element, "clientHeight", { configurable: true, value: metrics.clientHeight });
  Object.defineProperty(element, "scrollHeight", { configurable: true, value: metrics.scrollHeight });
  Object.defineProperty(element, "scrollTop", { configurable: true, value: metrics.scrollTop });
}

export function installStorage(name: "localStorage" | "sessionStorage"): void {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: {
      clear() {
        values.clear();
      },
      getItem(key: string) {
        return values.get(key) ?? null;
      },
      removeItem(key: string) {
        values.delete(key);
      },
      setItem(key: string, value: string) {
        values.set(key, value);
      },
    },
  });
}

export function setNavigatorUserAgent(userAgent: string): void {
  Object.defineProperty(window.navigator, "userAgent", {
    configurable: true,
    value: userAgent,
  });
}

export function installIntersectionObserverMock(): Readonly<{ reveal: (label: string) => void }> {
  const callbacks = new Map<string, (isIntersecting: boolean) => void>();
  class MockIntersectionObserver implements IntersectionObserver {
    readonly root = null;
    readonly rootMargin = "";
    readonly scrollMargin = "";
    readonly thresholds = [];
    readonly #callback: IntersectionObserverCallback;
    readonly #labels = new Set<string>();

    constructor(callback: IntersectionObserverCallback) {
      this.#callback = callback;
    }

    disconnect(): void {
      for (const label of this.#labels) {
        callbacks.delete(label);
      }
      this.#labels.clear();
    }

    observe(element: Element): void {
      const label = element.getAttribute("aria-label") ?? "";
      this.#labels.add(label);
      callbacks.set(label, (isIntersecting: boolean) => {
        this.#callback([intersectionEntry(element, isIntersecting)], this);
      });
    }

    takeRecords(): IntersectionObserverEntry[] {
      return [];
    }

    unobserve(element: Element): void {
      const label = element.getAttribute("aria-label") ?? "";
      this.#labels.delete(label);
      callbacks.delete(label);
    }
  }
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
  return {
    reveal(label: string): void {
      const callback = callbacks.get(label);
      if (callback === undefined) {
        throw new Error(`No observed column ${label}`);
      }
      callback(true);
    },
  };
}

function intersectionEntry(element: Element, isIntersecting: boolean): IntersectionObserverEntry {
  return {
    boundingClientRect: element.getBoundingClientRect(),
    intersectionRatio: isIntersecting ? 1 : 0,
    intersectionRect: isIntersecting ? element.getBoundingClientRect() : new DOMRectReadOnly(),
    isIntersecting,
    rootBounds: null,
    target: element,
    time: 0,
  };
}

export const workflow: BoardRouteWorkflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  version: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

export type BoardRouteWorkflow = Readonly<{
  workflow_id: string;
  display_name: string;
  description: string;
  version: number;
  is_project_default: boolean;
  valid_for_task_creation: boolean;
  validation_errors: readonly BoardRouteValidationError[];
}>;

type BoardRouteValidationError = Readonly<{
  code: string;
  message: string;
  node_id: string;
  edge_id: string;
  blocks_context: boolean;
}>;

export const workspace = {
  workspace_id: "workspace-1",
  display_name: "Main",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

type BoardRouteTaskActions = Readonly<{
  can_start: boolean;
  can_interrupt: boolean;
  interrupt_run_id: string;
  can_resume: boolean;
  resume_run_id: string;
  can_cancel: boolean;
  needs_detail_for_interrupt: boolean;
  needs_detail_for_resume: boolean;
  manual_move_target_node_ids: readonly string[];
}>;

type BoardRouteTaskStatus = Readonly<{
  kind: string;
  label: string;
  native_state: string;
  node_ids: readonly string[];
  run_ids: readonly string[];
  attention_types: readonly string[];
}>;

export type BoardRouteCard = Readonly<{
  task_id: string;
  short_id: string;
  title: string;
  body_preview: string;
  workflow_id: string;
  active_node_ids: readonly string[];
  source_workspace: typeof workspace;
  status: BoardRouteTaskStatus;
  actions: BoardRouteTaskActions;
  updated_at_unix_ms: number;
}>;

export const taskActions: BoardRouteTaskActions = {
  can_start: true,
  can_interrupt: false,
  interrupt_run_id: "",
  can_resume: false,
  resume_run_id: "",
  can_cancel: true,
  needs_detail_for_interrupt: false,
  needs_detail_for_resume: false,
  manual_move_target_node_ids: [],
};

const boardCards: readonly BoardRouteCard[] = [
  {
    task_id: "task-1",
    short_id: "T-1",
    title: "Write focused tests",
    body_preview: "Cover drag start",
    workflow_id: "workflow-1",
    active_node_ids: [],
    source_workspace: workspace,
    status: {
      kind: "backlog",
      label: "Backlog",
      native_state: "backlog",
      node_ids: [],
      run_ids: [],
      attention_types: [],
    },
    actions: taskActions,
    updated_at_unix_ms: 1,
  },
];

export function boardRoutes(
  response = boardResponse,
  nodePages: Readonly<
    Record<
      string,
      Readonly<{ cards: readonly (typeof boardResponse.board.cards)[number][]; nextPageToken?: string }>
    >
  > = {
    backlog: { cards: boardResponse.board.cards },
    "node-1": { cards: [] },
    done: { cards: [] },
  },
) {
  return [
    { method: "workflow.board.get", result: response },
    {
      method: "workflow.board.nodeCards.list",
      handler: (params: JsonValue) => {
        const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
        const page = nodePages[nodeID] ?? { cards: [] };
        return boardNodeCardsResponse(nodeID, page.cards, page.nextPageToken ?? "");
      },
    },
  ];
}

export function boardNodeCardsResponse(
  nodeID: string,
  cards: readonly (typeof boardResponse.board.cards)[number][],
  nextPageToken: string,
) {
  return {
    project_id: boardResponse.board.project_id,
    workflow_id: boardResponse.board.selected_workflow.workflow_id,
    node_id: nodeID,
    cards,
    next_page_token: nextPageToken,
    generated_at_unix_ms: 1,
  };
}

export function deferredBoardCardsResponse(): Readonly<{
  promise: Promise<ReturnType<typeof boardNodeCardsResponse>>;
  resolve: (response: ReturnType<typeof boardNodeCardsResponse>) => void;
}> {
  let resolve: (response: ReturnType<typeof boardNodeCardsResponse>) => void = () => undefined;
  const promise = new Promise<ReturnType<typeof boardNodeCardsResponse>>((innerResolve) => {
    resolve = innerResolve;
  });
  return { promise, resolve };
}

export function restoreStartViewTransition(descriptor: PropertyDescriptor | undefined): void {
  if (descriptor === undefined) {
    Reflect.deleteProperty(document, "startViewTransition");
    return;
  }
  Object.defineProperty(document, "startViewTransition", descriptor);
}

export function taskDetailResponseForCancel() {
  return {
    task: {
      summary: {
        id: "task-1",
        project_id: "project-1",
        workflow_id: "workflow-1",
        short_id: "T-1",
        title: "Task detail title",
        created_at_unix_ms: 1,
        updated_at_unix_ms: 2,
        done: false,
        canceled_at_unix_ms: 0,
      },
      project: { display_name: "Project" },
      workflow,
      body: "Cancel this task",
      source_workspace: workspace,
      status: {
        kind: "backlog",
        label: "Backlog",
        native_state: "active",
        node_ids: ["backlog"],
        run_ids: [],
        attention_types: [],
      },
      actions: {
        can_start: true,
        can_interrupt: false,
        interrupt_run_id: "",
        can_resume: false,
        resume_run_id: "",
        can_cancel: true,
        needs_detail_for_interrupt: false,
        needs_detail_for_resume: false,
        manual_move_target_node_ids: [],
      },
      attention: [],
      runs: [],
      transitions: [],
      comments: [],
    },
  };
}

export const emptyActivityResponse = {
  items: [],
  next_page_token: "",
  generated_at_unix_ms: 1,
};

export const boardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "proj", display_name: "Project" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [{ group_id: "group-1", key: "core", display_name: "Core", sort_order: 1, node_ids: ["node-1"] }],
    columns: [
      {
        node: { node_id: "backlog", key: "backlog", kind: "start", display_name: "Backlog" },
        group_id: "",
        sort_order: 0,
        is_backlog: true,
        is_done: false,
        task_count: 1,
      },
      {
        node: {
          node_id: "node-1",
          key: "implement",
          kind: "agent",
          display_name: "Implement",
          assignee_role: "coder",
        },
        group_id: "group-1",
        sort_order: 1,
        is_backlog: false,
        is_done: false,
        task_count: 0,
      },
      {
        node: { node_id: "done", key: "done", kind: "terminal", display_name: "Done" },
        group_id: "",
        sort_order: 99,
        is_backlog: false,
        is_done: true,
        task_count: 0,
      },
    ],
    cards: boardCards,
    done_preview: [],
    next_page_token: "",
    generated_at_unix_ms: 1,
  },
};

export const workflowDefinitionResponse = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      version: 1,
    },
    node_groups: [],
    nodes: [
      {
        id: "backlog",
        workflow_id: "workflow-1",
        key: "backlog",
        kind: "start",
        display_name: "Backlog",
      },
      {
        id: "node-1",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        subagent_role: "coder",
      },
      {
        id: "done",
        workflow_id: "workflow-1",
        key: "done",
        kind: "terminal",
        display_name: "Done",
      },
    ],
    transition_groups: [],
    edges: [],
  },
};

export function firstBoardCard(): (typeof boardResponse.board.cards)[number] {
  const card = boardResponse.board.cards[0];
  if (card === undefined) {
    throw new Error("board response test fixture has no cards");
  }
  return card;
}

export function firstBoardGroup(): (typeof boardResponse.board.groups)[number] {
  const group = boardResponse.board.groups[0];
  if (group === undefined) {
    throw new Error("board response test fixture has no groups");
  }
  return group;
}

export function boardColumnAt(index: number): (typeof boardResponse.board.columns)[number] {
  const column = boardResponse.board.columns[index];
  if (column === undefined) {
    throw new Error(`board response test fixture has no column at ${index.toString()}`);
  }
  return column;
}

export function doneBoardCard(): (typeof boardResponse.board.cards)[number] {
  const card = firstBoardCard();
  return {
    ...card,
    active_node_ids: ["done"],
    actions: {
      ...card.actions,
      can_start: false,
      manual_move_target_node_ids: [],
    },
    status: {
      ...card.status,
      kind: "done",
      label: "Done",
      native_state: "terminal",
      node_ids: ["done"],
    },
  };
}

export function nativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createTauriNativeBridge("macos");
  return {
    ...base,
    dialogs: {
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
  };
}

export function rejectingNativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createTauriNativeBridge("macos");
  return {
    ...base,
    dialogs: {
      async openWindow(options): Promise<void> {
        opened.push(options);
        throw new Error("Native dialog windows are unavailable in this shell.");
      },
    },
  };
}

export function nativeWindowBridge(onClose: () => void): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async closeCurrent(): Promise<void> {
        onClose();
      },
    },
  };
}

export function methodCallCount(calls: readonly { method: string }[], method: string): number {
  return calls.filter((call) => call.method === method).length;
}

export function sidebarWidthStyle(sidebar: HTMLElement): string {
  return sidebar.style.getPropertyValue("--app-sidebar-width");
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
