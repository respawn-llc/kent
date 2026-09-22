import { act, fireEvent, render as renderUI, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { QueryClient } from "@tanstack/react-query";

import {
  RpcError,
  type ApiService,
  rpcErrorCodes,
  type TaskDependencyDirection,
  type WorkspaceCatalogPage,
  type WorkspaceCatalogRow,
} from "@/api";
import { queryKeys, type AppLogger, type TaskSearchResult } from "@/app-facade";
import type { PreparedTaskDependency } from "@/shared/task-dependencies";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import type { SelectFieldPaging } from "@/ui";
import type * as UiModule from "@/ui";

interface TestSelectProps {
  onValueChange(value: string): void;
  options: readonly { label: ReactNode; value: string }[];
  paging?: SelectFieldPaging;
  value: string | undefined;
  disabled?: boolean;
}

interface TestState {
  select: TestSelectProps | undefined;
  create: ReturnType<typeof vi.fn<ApiService["createTask"]>>;
  loggerAppend: ReturnType<typeof vi.fn<AppLogger["append"]>>;
  labels: { labels: readonly { id: string; name: string }[] } | undefined;
  searchResults: readonly TaskSearchResult[];
  statusDismiss: ReturnType<typeof vi.fn>;
  statusPush: ReturnType<typeof vi.fn>;
}

const state = vi.hoisted((): TestState => ({
  select: undefined,
  create: vi.fn(async () => ({
    id: "task-created",
    shortID: "KENT-42",
    title: "Task",
    workflowID: "workflow-1",
  })),
  loggerAppend: vi.fn(async () => undefined),
  labels: { labels: [] },
  searchResults: [],
  statusDismiss: vi.fn(),
  statusPush: vi.fn(),
}));

vi.mock("react-i18next", async (original) => ({
  ...(await original()),
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/app-facade", async (original) => ({
  ...(await original()),
  useStatusController: () => ({ dismiss: state.statusDismiss, push: state.statusPush }),
  useTaskSearch: () => ({
    displayedQuery: null,
    normalizedTooShort: false,
    paginationUsesVisibleData: true,
    request: {
      data: undefined,
      error: null,
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchNextPageError: false,
      isFetching: false,
      isFetchingNextPage: false,
      refetch: vi.fn(),
    },
    results: state.searchResults,
    searchable: state.searchResults.length > 0,
  }),
  useTextFieldSubmitShortcut: () => undefined,
}));
vi.mock("@/shared/labels", () => ({
  LabelChooser: () => null,
  ProjectLabelsProvider: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  orderedAssignedLabels: () => [],
  useProjectLabelCatalog: () => ({ data: state.labels }),
}));
vi.mock("@/shared/native-dialog", () => ({
  NativeDialogWindow: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
}));
vi.mock("@/ui", async (importOriginal) => ({
  ...(await importOriginal<typeof UiModule>()),
  Badge: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  Button: ({ children, ...props }: ComponentProps<"button">) => <button {...props}>{children}</button>,
  Dialog: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  FieldShell: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  InfiniteListBoundary: ({
    state: boundary,
  }: {
    state: { state: string; onRetry?: (() => void) | undefined };
  }) => {
    const { onRetry } = boundary;
    return boundary.state === "error" ? <button onClick={onRetry}>exact-retry</button> : null;
  },
  LabelChooser: () => null,
  SelectField: (props: TestSelectProps) => {
    state.select = props;
    return (
      <>
        {props.options.map((option) => (
          <button
            disabled={props.disabled}
            key={option.value}
            onClick={() => {
              props.onValueChange(option.value);
            }}
          >
            {option.label}
          </button>
        ))}
        {props.paging?.nextBoundary?.state === "error" ? (
          <button onClick={props.paging.nextBoundary.onRetry}>edge-retry</button>
        ) : null}
      </>
    );
  },
  TextArea: (props: ComponentProps<"textarea"> & { label: string }) => (
    <textarea aria-label={props.label} {...props} />
  ),
  TextInput: ({ error, label, ...props }: ComponentProps<"input"> & { error?: ReactNode; label: string }) => (
    <>
      <input aria-label={label} {...props} />
      {error}
    </>
  ),
  cx: (...values: readonly (string | undefined)[]) => values.filter(Boolean).join(" "),
}));

import { NewTaskForm } from "./NewTaskDialog";

const row = (id: string, isDefault = false): WorkspaceCatalogRow => ({
  id,
  isDefault,
  name: id,
  rootPath: `/${id}`,
});
const page = (workspaces = [row("default", true), row("other")]): WorkspaceCatalogPage => ({
  projectID: "project-1",
  offset: 0,
  workspaces,
  nextOffset: 100,
});
const props = {
  boardQueryWorkflowID: "workflow-1",
  initialSourceWorkspaceID: "source",
  navigator: createTestSidebarNavigator(),
  projectID: "project-1",
  workflowID: "workflow-1",
};
let services: ReturnType<typeof createTestServices>;
let client: QueryClient;
let listWorkspaces: ReturnType<typeof vi.fn<ApiService["listWorkspaces"]>>;
let getProjectWorkspace: ReturnType<typeof vi.fn<ApiService["getProjectWorkspace"]>>;
function render(element: ReactElement) {
  return renderUI(element, {
    wrapper: ({ children }) => (
      <TestAppProviders services={services} queryClient={client}>
        {children}
      </TestAppProviders>
    ),
  });
}
function loadCatalog(workspaces?: WorkspaceCatalogRow[]) {
  act(() => {
    client.setQueryData(queryKeys.projectWorkspaceCatalog("project-1"), {
      pages: [page(workspaces)],
      pageParams: [0],
    });
  });
}
function loadExact(data: Awaited<ReturnType<ApiService["getProjectWorkspace"]>>) {
  act(() => {
    client.setQueryData(queryKeys.projectWorkspace("project-1", "source"), data);
  });
}
function loadAttachedCatalog() {
  loadCatalog();
  loadExact({ kind: "attached", workspace: row("source") });
}
function rerender(view: ReturnType<typeof render>) {
  view.rerender(<NewTaskForm {...props} />);
}

describe("New Task Workspace catalog integration", () => {
  beforeEach(() => {
    services = createTestServices([]);
    client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
    });
    listWorkspaces = vi
      .spyOn(services.api, "listWorkspaces")
      .mockImplementation(async () => new Promise(() => undefined));
    getProjectWorkspace = vi
      .spyOn(services.api, "getProjectWorkspace")
      .mockImplementation(async () => new Promise(() => undefined));
    vi.spyOn(services.api, "createTask").mockImplementation(state.create);
    vi.spyOn(services.logger, "append").mockImplementation(state.loggerAppend);
    state.create.mockClear();
    state.loggerAppend.mockClear();
    state.labels = { labels: [] };
    state.searchResults = [];
    state.statusDismiss.mockClear();
    state.statusPush.mockClear();
    state.select = undefined;
  });

  it.each(["catalog-first", "exact-first"])(
    "selects the initiating Workspace for %s response order",
    (order) => {
      if (order === "catalog-first") loadCatalog();
      else {
        loadExact({ kind: "attached", workspace: row("source") });
      }
      const view = render(<NewTaskForm {...props} />);
      if (order === "catalog-first") {
        loadExact({ kind: "attached", workspace: row("source") });
      } else loadCatalog();
      rerender(view);
      expect(state.select?.value).toBe("source");
    },
  );

  it.each(["catalog-first", "exact-first"])(
    "selects the default only after detached initiating and catalog results arrive %s",
    (order) => {
      if (order === "catalog-first") loadCatalog();
      else {
        loadExact({ kind: "not_attached" });
      }
      const view = render(<NewTaskForm {...props} />);
      expect(state.select?.value).toBeUndefined();

      if (order === "catalog-first") {
        loadExact({ kind: "not_attached" });
      } else loadCatalog();
      rerender(view);

      expect(state.select?.value).toBe("default");
    },
  );

  it("preserves Task drafts when a late initiating Workspace result commits selection", () => {
    loadCatalog();
    const view = render(<NewTaskForm {...props} />);
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), {
      target: { value: "Draft title" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "task.body" }), {
      target: { value: "Draft body" },
    });
    expect(state.select?.value).toBeUndefined();

    loadExact({ kind: "attached", workspace: row("source") });
    rerender(view);

    expect(state.select?.value).toBe("source");
    expect(screen.getByRole("textbox", { name: "task.name" })).toHaveValue("Draft title");
    expect(screen.getByRole("textbox", { name: "task.body" })).toHaveValue("Draft body");
    expect(screen.getByRole("button", { name: "task.create" })).toBeEnabled();
  });

  it("restarts once from the default-first page when the retained catalog window starts after zero", async () => {
    client.setQueryData(queryKeys.projectWorkspaceCatalog("project-1"), {
      pages: [
        {
          projectID: "project-1",
          offset: 200,
          workspaces: [row("retained")],
          nextOffset: 300,
        },
      ],
      pageParams: [200],
    });
    loadExact({ kind: "not_attached" });
    listWorkspaces.mockResolvedValue(page());

    render(<NewTaskForm {...props} />);

    await waitFor(() => {
      expect(state.select?.value).toBe("default");
    });
    expect(listWorkspaces).toHaveBeenCalledExactlyOnceWith("project-1", 0);
  });

  it("keeps attached exact selection usable through first-page failure and Retry", async () => {
    loadExact({ kind: "attached", workspace: row("source") });
    listWorkspaces.mockRejectedValueOnce(new Error("catalog")).mockResolvedValue(page());
    render(<NewTaskForm {...props} />);
    expect(state.select?.value).toBe("source");
    await waitFor(() => {
      expect(state.select?.paging?.initialBoundary?.state).toBe("error");
    });
    const initialBoundary = state.select?.paging?.initialBoundary;
    if (initialBoundary?.state === "error") {
      act(() => {
        initialBoundary.onRetry();
      });
    }
    await waitFor(() => {
      expect(state.select?.paging?.initialBoundary).toBeUndefined();
    });
    expect(listWorkspaces).toHaveBeenCalledTimes(2);
    expect(state.select?.value).toBe("source");
  });

  it("retries exact failure without fallback and never overwrites an explicit choice", async () => {
    loadCatalog();
    getProjectWorkspace.mockRejectedValueOnce(new Error("exact"));
    const view = render(<NewTaskForm {...props} />);
    expect(state.select?.value).toBeUndefined();
    fireEvent.click(await screen.findByRole("button", { name: "exact-retry" }));
    await waitFor(() => {
      expect(getProjectWorkspace).toHaveBeenCalledTimes(2);
    });
    fireEvent.click(screen.getByRole("button", { name: "other" }));
    loadExact({ kind: "attached", workspace: row("source") });
    rerender(view);
    expect(state.select?.value).toBe("other");
  });

  it("keeps one loaded Workspace selectable while the exact read remains failed", async () => {
    loadCatalog([row("only", true)]);
    getProjectWorkspace.mockRejectedValueOnce(new Error("exact"));
    render(<NewTaskForm {...props} />);

    expect(state.select?.disabled).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "only" }));

    expect(state.select?.value).toBe("only");
    expect(screen.getByRole("button", { name: "task.create" })).toBeEnabled();
    expect(await screen.findByRole("button", { name: "exact-retry" })).toBeInTheDocument();
  });

  it("pins initiating and evicted selected rows once while fresh loaded rows replace snapshots", () => {
    loadCatalog([row("selected")]);
    loadExact({ kind: "attached", workspace: row("source") });
    const view = render(<NewTaskForm {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "selected" }));
    act(() => {
      client.setQueryData(queryKeys.projectWorkspaceCatalog("project-1"), {
        pages: [{ projectID: "project-1", offset: 400, workspaces: [row("retained")], nextOffset: null }],
        pageParams: [400],
      });
    });
    rerender(view);
    expect(state.select?.options.map(({ value }) => value)).toEqual(["source", "selected", "retained"]);
  });

  it("falls back only after typed detachment and retries a failed page edge", async () => {
    loadCatalog();
    loadExact({ kind: "not_attached" });
    listWorkspaces.mockRejectedValueOnce(new Error("edge")).mockResolvedValue({
      projectID: "project-1",
      offset: 100,
      workspaces: [row("next")],
      nextOffset: null,
    });
    render(<NewTaskForm {...props} />);
    expect(state.select?.value).toBe("default");
    act(() => {
      state.select?.paging?.onLoadNext();
    });
    fireEvent.click(await screen.findByRole("button", { name: "edge-retry" }));
    await waitFor(() => {
      expect(state.select?.options.map(({ value }) => value)).toContain("next");
    });
    expect(listWorkspaces).toHaveBeenCalledTimes(2);
    expect(state.select?.value).toBe("default");
  });

  it("propagates missing Project and submits the displayed Workspace identity", async () => {
    loadCatalog();
    loadExact({ kind: "not_attached" });
    const navigator = createTestSidebarNavigator();
    const view = render(<NewTaskForm {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "other" }));
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), { target: { value: "Task" } });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    await vi.waitFor(() => {
      expect(state.create).toHaveBeenCalledWith(expect.objectContaining({ sourceWorkspaceID: "other" }));
    });
    view.unmount();
    client.removeQueries({ queryKey: queryKeys.projectWorkspace("project-1", "source") });
    getProjectWorkspace.mockRejectedValueOnce(
      new RpcError({
        code: rpcErrorCodes.projectNotFound,
        message: "gone",
        method: "project.workspace.get",
      }),
    );
    render(<NewTaskForm {...props} navigator={navigator} />);
    await vi.waitFor(() => {
      expect(navigator.back).toHaveBeenCalled();
    });
  });

  it("shows ordinary and Task Detail-originated prepared dependencies and delegates their actions", () => {
    loadAttachedCatalog();
    const view = render(<NewTaskForm {...props} />);
    expect(screen.getByText("task.dependencies")).toBeInTheDocument();
    expect(screen.queryByTestId(/^dependency-row-/)).not.toBeInTheDocument();
    view.unmount();
    const navigator = createTestSidebarNavigator();
    render(
      <NewTaskForm
        {...props}
        initialPreparedDependency={preparedDependency("blocks", "task-origin")}
        navigator={navigator}
      />,
    );
    fireEvent.click(screen.getByTestId("dependency-row-task-origin"));
    expect(navigator.push).toHaveBeenCalledWith({ kind: "taskDetail", taskID: "task-origin" });
    fireEvent.click(screen.getByTestId("dependency-remove-task-origin"));
    expect(screen.queryByTestId("dependency-row-task-origin")).not.toBeInTheDocument();
  });

  it("pushes a stacked child without an unsaved-parent relationship and captures the complete Draft", () => {
    loadAttachedCatalog();
    const navigator = createTestSidebarNavigator();
    render(<NewTaskForm {...props} navigator={navigator} />);
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), {
      target: { value: "Parent title" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "task.body" }), {
      target: { value: "Parent body" },
    });
    fireEvent.click(screen.getByTestId("dependency-add-blocks"));
    fireEvent.click(screen.getByRole("button", { name: "task.dependenciesCreateTask" }));

    expect(navigator.push).toHaveBeenCalledWith({
      boardQueryWorkflowID: "workflow-1",
      initialSourceWorkspaceID: "source",
      kind: "newTask",
      parentReturnDirection: "blocks",
      projectID: "project-1",
      workflowID: "workflow-1",
    });
    const capture = vi.mocked(navigator.registerCapture).mock.lastCall?.[0];
    if (capture === undefined) throw new Error("Expected New Task Draft capture.");
    expect(capture()).toEqual({
      formValues: {
        body: "Parent body",
        sourceWorkspaceID: "source",
        title: "Parent title",
      },
      preparedDependencies: [],
      selectedLabelIDs: [],
    });
  });

  it("restores the authored Draft with picker state closed and recomputes validation on submission", async () => {
    loadAttachedCatalog();
    state.labels = undefined;
    const retainedState = {
      formValues: { body: "Body", sourceWorkspaceID: "source", title: "" },
      preparedDependencies: [preparedDependency("blocked-by", "task-restored")],
      selectedLabelIDs: ["label-restored"],
    };
    const view = render(<NewTaskForm {...props} retainedState={retainedState} />);
    expect(screen.getByRole("textbox", { name: "task.name" })).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "task.body" })).toHaveValue("Body");
    expect(screen.getByTestId("dependency-row-task-restored")).toBeInTheDocument();
    expect(screen.queryByText("form.required")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "task.create" })).toBeDisabled();
    state.labels = { labels: [{ id: "label-restored", name: "Restored" }] };
    view.rerender(<NewTaskForm {...props} retainedState={retainedState} />);
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    expect(await screen.findByText("form.required")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), { target: { value: "Task" } });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    await vi.waitFor(() => {
      expect(state.create).toHaveBeenCalledWith(
        expect.objectContaining({
          labelIDs: ["label-restored"],
        }),
      );
    });
  });

  it("submits every prepared relationship atomically and returns through one Back operation", async () => {
    loadAttachedCatalog();
    const navigator = createTestSidebarNavigator();
    render(
      <NewTaskForm
        {...props}
        navigator={navigator}
        retainedState={{
          formValues: { body: "", sourceWorkspaceID: "source", title: "" },
          preparedDependencies: [
            preparedDependency("blocks", "task-origin"),
            preparedDependency("blocked-by", "task-blocked"),
          ],
          selectedLabelIDs: [],
        }}
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), {
      target: { value: "Task" },
    });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    await vi.waitFor(() => {
      expect(state.create).toHaveBeenCalledWith(
        expect.objectContaining({
          dependencyIntents: [
            { relatedTaskID: "task-origin", newTaskRole: "blocker" },
            { relatedTaskID: "task-blocked", newTaskRole: "blocked" },
          ],
        }),
      );
    });
    expect(navigator.back).toHaveBeenCalledOnce();
    expect(navigator.close).not.toHaveBeenCalled();
    expect(navigator.replace).not.toHaveBeenCalled();
  });

  it("stages searched dependencies through the 49→50 limit and cannot select a 51st", async () => {
    loadAttachedCatalog();
    state.searchResults = [candidate("task-49"), candidate("task-50"), candidate("task-51")];
    const user = userEvent.setup();
    render(
      <NewTaskForm
        {...props}
        retainedState={{
          formValues: { body: "", sourceWorkspaceID: "source", title: "" },
          preparedDependencies: Array.from({ length: 48 }, (_, index) =>
            preparedDependency("blocked-by", `task-${String(index)}`),
          ),
          selectedLabelIDs: [],
        }}
      />,
    );
    await user.click(screen.getByTestId("dependency-add-blocked-by"));
    await user.click(screen.getByTestId("dependency-candidate-task-49"));
    expect(screen.getByTestId("dependency-candidate-task-50")).toBeInTheDocument();
    await user.click(screen.getByTestId("dependency-candidate-task-50"));
    expect(screen.getByTestId("dependency-row-task-49")).toBeInTheDocument();
    expect(screen.getByTestId("dependency-row-task-50")).toBeInTheDocument();
    expect(screen.queryByTestId("dependency-candidate-task-51")).not.toBeInTheDocument();
    expect(screen.getByTestId("dependency-add-blocked-by")).toBeDisabled();
  });

  it("returns an active stacked child summary with backlog status and keeps Back available while pending", async () => {
    loadAttachedCatalog();
    const navigator = createTestSidebarNavigator();
    render(<NewTaskForm {...props} navigator={navigator} parentReturnDirection="blocked-by" />);
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), {
      target: { value: "Child" },
    });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    await vi.waitFor(() => {
      expect(navigator.back).toHaveBeenCalledWith({
        kind: "newTaskCreated",
        direction: "blocked-by",
        task: {
          id: "task-created",
          shortID: "KENT-42",
          status: {
            kind: "backlog",
            nativeState: "active",
            nodeIDs: [],
            attentionTypes: [],
          },
          title: "Task",
          workflowID: "workflow-1",
        },
      });
    });
  });

  it("completes accepted creation after leaving without following stale parent navigation", async () => {
    loadAttachedCatalog();
    const response = deferred<Awaited<ReturnType<ApiService["createTask"]>>>();
    state.create.mockReturnValueOnce(response.promise);
    const navigator = createTestSidebarNavigator();
    const onCreated = vi.fn();
    const view = render(
      <NewTaskForm
        {...props}
        navigator={navigator}
        onCreated={onCreated}
        parentReturnDirection="blocked-by"
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), { target: { value: "Child" } });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));
    await waitFor(() => {
      expect(state.create).toHaveBeenCalledOnce();
    });
    view.unmount();
    vi.mocked(navigator.back).mockReturnValue("stale");
    await act(async () => {
      response.resolve({ id: "task-created", shortID: "KENT-42", title: "Child", workflowID: "workflow-1" });
    });
    await waitFor(() => {
      expect(navigator.back).toHaveBeenCalledOnce();
    });
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("presents reciprocal rejection as product copy and preserves authored dependencies", async () => {
    loadAttachedCatalog();
    state.searchResults = [candidate("task-related")];
    state.create.mockRejectedValueOnce(
      new RpcError({
        code: rpcErrorCodes.workflowTaskDependency,
        message: "workflow task dependency error: reciprocal_dependency",
        method: "workflow.task.create",
        data: {
          type: "workflow_task_dependency_error",
          reason: "reciprocal_dependency",
          blocker_task_id: "task-created",
          blocked_task_id: "task-related",
        },
      }),
    );
    const user = userEvent.setup();
    render(
      <NewTaskForm
        {...props}
        retainedState={{
          formValues: { body: "Keep body", sourceWorkspaceID: "source", title: "Keep title" },
          preparedDependencies: [preparedDependency("blocks", "task-related")],
          selectedLabelIDs: [],
        }}
      />,
    );
    const addDependency = screen.getByTestId("dependency-add-blocked-by");
    await user.click(addDependency);
    await user.click(screen.getByTestId("dependency-candidate-task-related"));
    fireEvent.submit(addDependency);
    await vi.waitFor(() => {
      expect(state.statusPush).toHaveBeenCalledOnce();
    });
    expect(state.loggerAppend.mock.lastCall?.[2]).toHaveProperty("error");
    expect(state.loggerAppend.mock.lastCall?.[2]).toHaveProperty("reason", "reciprocal_dependency");
    expect(screen.getAllByTestId("dependency-row-task-related")).toHaveLength(2);
    const [removeDependency] = screen.getAllByTestId("dependency-remove-task-related");
    if (removeDependency === undefined) throw new Error("Expected a removable prepared dependency.");
    await user.click(removeDependency);
    fireEvent.submit(addDependency);
    await vi.waitFor(() => {
      expect(state.statusDismiss).toHaveBeenCalledTimes(2);
    });
  });

  it("submits Project-scoped creation without inventing a Workflow selection", async () => {
    loadCatalog();
    loadExact({ kind: "not_attached" });
    render(<NewTaskForm {...props} boardQueryWorkflowID={undefined} workflowID={undefined} />);

    fireEvent.change(screen.getByRole("textbox", { name: "task.name" }), {
      target: { value: "Project-scoped task" },
    });
    fireEvent.click(screen.getByRole("button", { name: "task.create" }));

    await vi.waitFor(() => {
      expect(state.create).toHaveBeenCalledWith({
        body: "",
        dependencyIntents: [],
        labelIDs: [],
        projectID: "project-1",
        sourceWorkspaceID: "default",
        title: "Project-scoped task",
      });
    });
  });
});

function preparedDependency(direction: TaskDependencyDirection, taskID: string): PreparedTaskDependency {
  return {
    direction,
    taskID,
    shortID: taskID,
    title: taskID,
    workflowID: "workflow-1",
    status: {
      kind: "backlog",
      nativeState: "active",
      nodeIDs: [],
      attentionTypes: [],
    },
  };
}

function candidate(taskID: string): TaskSearchResult {
  const dependency = preparedDependency("blocked-by", taskID);
  return {
    key: taskID,
    group: {
      projectID: "project-1",
      projectKey: "KENT",
      totalHitCount: 1,
      hits: [
        {
          ordinal: 1,
          source: { kind: "title" },
          literal: { before: "", match: taskID, after: "", leftTruncated: false, rightTruncated: false },
        },
      ],
      ...dependency,
    },
  };
}
