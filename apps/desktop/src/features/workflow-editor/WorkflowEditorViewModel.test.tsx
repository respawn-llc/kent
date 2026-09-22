import { RegistryProvider, useAtomValue, useAtomSuspense } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import {
  defaultWorkflowExecutionTargetPolicy,
  emptyWorkflowDerivedWiring,
  type WorkflowDefinition,
  type WorkflowGraphSavePreview,
} from "@/api";
import { queryKeys } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { appI18n } from "@/i18n";
import {
  createWorkflowEditorViewModel,
  useWorkflowEditorActions,
  useWorkflowGraphEditorActions,
} from "./WorkflowEditorViewModel";
import { useWorkflowEditorDraftView } from "./useWorkflowEditorView";

function definition(version = 1, name = "Workflow"): WorkflowDefinition {
  return {
    derivedWiring: emptyWorkflowDerivedWiring,
    edges: [],
    nodeGroups: [],
    nodes: [],
    transitionGroups: [],
    workflow: {
      description: "",
      executionTargetPolicy: defaultWorkflowExecutionTargetPolicy,
      id: "11111111-1111-4111-8111-111111111111",
      name,
      version,
    },
  };
}

function fixture(
  beforeMount?: (context: {
    services: ReturnType<typeof createTestServices>;
    client: QueryClient;
    source: WorkflowDefinition;
  }) => void,
) {
  const services = createTestServices([]);
  const source = definition();
  const load = vi.spyOn(services.api, "getWorkflow").mockResolvedValue(source);
  vi.spyOn(services.api, "validateWorkflow").mockResolvedValue({ valid: true, errors: [] });
  const links = vi.spyOn(services.api, "listProjectWorkflowLinks");
  const projectSubscribe = vi.spyOn(services.api, "subscribeProject");
  const close = vi.fn();
  const subscribe = vi.spyOn(services.api, "subscribeWorkflow").mockReturnValue({ close });
  const validation: Awaited<ReturnType<typeof services.api.validateWorkflowGraphDraft>> = {
    draft: { valid: true, errors: [] },
    execution: { valid: true, errors: [] },
    derivedWiring: emptyWorkflowDerivedWiring,
  };
  const validate = vi.spyOn(services.api, "validateWorkflowGraphDraft").mockResolvedValue(validation);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  beforeMount?.({ services, client, source });
  const model = createWorkflowEditorViewModel({
    services,
    client,
    workflowID: source.workflow.id,
    projectID: null,
    t: appI18n.t,
    push: vi.fn(),
  });
  return { model, services, source, load, links, client, validate, subscribe, projectSubscribe, close };
}

function setup(beforeMount?: Parameters<typeof fixture>[0]) {
  const context = fixture(beforeMount);
  const { model } = context;
  const view = renderHook(
    () => ({
      state: useAtomValue(model.state),
      data: useAtomValue(model.data),
      graph: useAtomValue(model.graph),
      saveState: useAtomValue(model.saveState),
      observation: useAtomSuspense(model.workflowObservation).value,
      ...useWorkflowGraphEditorActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  return { ...view, ...context };
}

it("retains a loaded Draft when a refresh fails without sending global-library Project requests", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.state.draftState?.source).toEqual(view.source);
  });
  act(() => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Local", description: "" });
  });
  view.load.mockRejectedValue(new Error("Unavailable"));
  act(() => {
    view.result.current.retryLoad(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.data.workflowQuery.isError).toBe(true);
  });
  expect(view.result.current.state.draftState?.draft.workflow.name).toBe("Local");
  expect(view.links).not.toHaveBeenCalled();
  expect(view.projectSubscribe).not.toHaveBeenCalled();
});

it("loads and edits settings without activating graph validation, wiring, or layout", async () => {
  const { services, source, validate, client, model } = fixture();
  const wiring = vi
    .spyOn(services.api, "deriveWorkflowGraphWiring")
    .mockResolvedValue(emptyWorkflowDerivedWiring);
  const preview = vi
    .spyOn(services.api, "previewWorkflowGraphSave")
    .mockRejectedValue(new Error("Save failed"));
  const view = renderHook(
    () => ({
      view: useWorkflowEditorDraftView(model),
      ...useWorkflowEditorActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.view?.state.source).toEqual(source);
  });
  expect(validate).not.toHaveBeenCalled();
  await act(async () => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Settings edit", description: "" });
    view.result.current.save(undefined);
  });
  expect(preview).toHaveBeenCalledTimes(1);
  expect(view.result.current.view?.draft.workflow.name).toBe("Settings edit");
  expect(validate).not.toHaveBeenCalled();
  expect(wiring).not.toHaveBeenCalled();
  expect(
    client
      .getQueryCache()
      .findAll({ queryKey: queryKeys.allWorkflowGraphLayouts })
      .every((query) => query.state.fetchStatus === "idle" && query.state.data === undefined),
  ).toBe(true);
});

it("retains edits when a newer saved Workflow arrives and acknowledges that conflict explicitly", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.state.draftState?.source).toEqual(view.source);
  });
  act(() => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Local", description: "" });
    view.client.setQueryData(queryKeys.workflowDefinition(view.source.workflow.id), definition(2, "Remote"));
  });
  await waitFor(() => {
    expect(view.result.current.state.draftState?.conflict?.workflow.version).toBe(2);
  });
  expect(view.result.current.state.draftState?.draft.workflow.name).toBe("Local");
  expect(view.result.current.state.draftState?.acknowledgedConflictVersion).toBeNull();
  act(() => {
    view.result.current.edit({ type: "keepEditing" });
  });
  await waitFor(() => {
    expect(view.result.current.state.draftState?.conflict).toBeNull();
  });
  expect(view.result.current.state.draftState?.acknowledgedConflictVersion).toBe(2);
});

it("replaces a clean Draft with the latest saved Workflow", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.state.draftState?.source).toEqual(view.source);
  });
  act(() => {
    view.client.setQueryData(queryKeys.workflowDefinition(view.source.workflow.id), definition(2, "Remote"));
  });
  await waitFor(() => {
    expect(view.result.current.state.draftState?.draft.workflow.name).toBe("Remote");
  });
  expect(view.result.current.state.draftState?.source.workflow.version).toBe(2);
  expect(view.result.current.state.draftState?.conflict).toBeNull();
});

it("validates current metadata without changing the graph and never validates an unavailable Draft", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.validate).toHaveBeenCalledTimes(1);
  });
  expect(view.validate.mock.calls[0]?.[0].metadata?.name).toBe("Workflow");
  act(() => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Changed", description: "" });
  });
  await waitFor(() => {
    expect(view.validate).toHaveBeenCalledTimes(2);
  });
  expect(view.validate.mock.calls[1]?.[0].metadata?.name).toBe("Changed");
  expect(view.result.current.state.draftState?.graphVersion).toBe(0);
});

it("keeps unavailable reads idle and reuses available validation from the existing Query cache", async () => {
  const response = deferred<WorkflowDefinition>();
  const cached = { draft: { valid: false, errors: [] }, derivedWiring: emptyWorkflowDerivedWiring };
  const view = setup(({ services, client, source }) => {
    vi.spyOn(services.api, "getWorkflow").mockReturnValue(response.promise);
    client.setQueryData(
      queryKeys.workflowDraftValidation(
        source.workflow.id,
        source.workflow.version,
        0,
        JSON.stringify({
          description: source.workflow.description,
          name: source.workflow.name,
          executionTargetPolicy: source.workflow.executionTargetPolicy,
        }),
      ),
      cached,
    );
  });
  await act(async () => undefined);
  expect(view.validate).not.toHaveBeenCalled();
  expect(view.result.current.state.draftState).toBeNull();
  expect(
    view.client.getQueryState(queryKeys.workflowDraftValidation(view.source.workflow.id, null, null, null))
      ?.fetchStatus,
  ).toBe("idle");
  await act(async () => {
    response.resolve(view.source);
    await response.promise;
  });
  await waitFor(() => {
    expect(view.result.current.graph.draftValidation).toEqual(cached.draft);
  });
  expect(view.validate).not.toHaveBeenCalled();
});

it("derives wiring from current dirty graph inputs instead of reusing clean validation wiring", async () => {
  const view = setup();
  const derive = vi
    .spyOn(view.services.api, "deriveWorkflowGraphWiring")
    .mockResolvedValue(emptyWorkflowDerivedWiring);
  await waitFor(() => {
    expect(view.validate).toHaveBeenCalledTimes(1);
  });
  expect(derive).not.toHaveBeenCalled();
  act(() => {
    view.result.current.edit({ type: "addNode", input: { id: "node-1", kind: "agent", name: "First" } });
  });
  await waitFor(() => {
    expect(derive).toHaveBeenCalledTimes(1);
  });
  act(() => {
    view.result.current.edit({ type: "editAgentNode", nodeID: "node-1", patch: { name: "Second" } });
  });
  await waitFor(() => {
    expect(derive).toHaveBeenCalledTimes(2);
  });
  expect(derive.mock.calls[1]?.[0].graph.nodes[0]?.name).toBe("Second");
  expect(view.validate).toHaveBeenCalledTimes(1);
});

it("keeps Draft and execution validation distinct", async () => {
  const view = setup();
  const results: Awaited<ReturnType<typeof view.services.api.validateWorkflowGraphDraft>> = {
    draft: { valid: true, errors: [] },
    execution: { valid: false, errors: [] },
    derivedWiring: emptyWorkflowDerivedWiring,
  };
  view.validate.mockResolvedValue(results);
  await waitFor(() => {
    expect(view.result.current.state.draftState).not.toBeNull();
  });
  act(() => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Revalidate", description: "" });
  });
  await waitFor(() => {
    expect(view.result.current.graph.executionValidation?.valid).toBe(false);
  });
  expect(view.result.current.graph.draftValidation?.valid).toBe(true);
});

it("admits one Save for repeated same-turn actions using the current Draft", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.previewWorkflowGraphSave>>>();
  const preview = vi.spyOn(view.services.api, "previewWorkflowGraphSave").mockReturnValue(response.promise);
  await waitFor(() => {
    expect(view.result.current.state.draftState).not.toBeNull();
  });
  await act(async () => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Submitted", description: "" });
    view.result.current.save(undefined);
    view.result.current.save(undefined);
  });
  expect(preview).toHaveBeenCalledTimes(1);
  expect(preview.mock.calls[0]?.[0].metadata?.name).toBe("Submitted");
  await act(async () => {
    response.reject(new Error("Request failed"));
    await response.promise.catch(() => undefined);
  });
  expect(view.result.current.state.draftState?.draft.workflow.name).toBe("Submitted");
  await waitFor(() => {
    expect(view.result.current.saveState.error).not.toBeNull();
  });
});

it("does not submit an unready or unchanged Draft", async () => {
  const view = setup();
  const preview = vi.spyOn(view.services.api, "previewWorkflowGraphSave");
  act(() => {
    view.result.current.save(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.state.draftState).not.toBeNull();
  });
  act(() => {
    view.result.current.save(undefined);
  });
  expect(preview).not.toHaveBeenCalled();
});

it("blocks graph Save on Draft validation but not on execution-only validation", async () => {
  const view = setup();
  const preview = vi
    .spyOn(view.services.api, "previewWorkflowGraphSave")
    .mockRejectedValue(new Error("Preview unavailable"));
  vi.spyOn(view.services.api, "deriveWorkflowGraphWiring").mockResolvedValue(emptyWorkflowDerivedWiring);
  await waitFor(() => {
    expect(view.result.current.state.draftState).not.toBeNull();
  });
  view.validate.mockResolvedValue({
    draft: { valid: false, errors: [] },
    execution: { valid: false, errors: [] },
    derivedWiring: emptyWorkflowDerivedWiring,
  });
  await act(async () => {
    view.result.current.edit({ type: "addNode", input: { id: "node-1", kind: "agent" } });
    view.result.current.save(undefined);
  });
  expect(preview).not.toHaveBeenCalled();
  expect(view.result.current.saveState.validation?.draft?.valid).toBe(false);
  expect(view.result.current.state.draftState?.draft.nodes).toHaveLength(1);
  view.validate.mockResolvedValue({
    draft: { valid: true, errors: [] },
    execution: { valid: false, errors: [] },
    derivedWiring: emptyWorkflowDerivedWiring,
  });
  await act(async () => {
    view.result.current.save(undefined);
  });
  expect(preview).toHaveBeenCalledTimes(1);
});

it("completes an accepted Save and invalidates saved content after the destination is disposed", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.saveWorkflowGraph>>>();
  const saved = definition(2, "Submitted");
  const preview: WorkflowGraphSavePreview = {
    changed: true,
    currentVersion: 1,
    validationResults: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
    blockers: [],
    confirmationRequired: false,
    canSave: true,
    impact: {
      removedNodeGroupCount: 0,
      removedNodeCount: 0,
      removedTransitionGroupCount: 0,
      removedEdgeCount: 0,
      removedEntities: [],
      nodeTaskReferenceCount: 0,
      edgeTaskReferenceCount: 0,
      activeCurrentNodeCount: 0,
      pendingApprovalCount: 0,
      startNodeChangeCount: 0,
      lastTerminalChangeCount: 0,
      taskReferencedNodeKindChangeCount: 0,
    },
  };
  vi.spyOn(view.services.api, "previewWorkflowGraphSave").mockResolvedValue(preview);
  const save = vi.spyOn(view.services.api, "saveWorkflowGraph").mockReturnValue(response.promise);
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await waitFor(() => {
    expect(view.result.current.state.draftState).not.toBeNull();
  });
  await act(async () => {
    view.result.current.edit({ type: "editWorkflowMetadata", name: "Submitted", description: "" });
    view.result.current.save(undefined);
  });
  expect(save).toHaveBeenCalledTimes(1);
  vi.useFakeTimers();
  try {
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    await act(async () => {
      response.resolve({ ...preview, saved: true, definition: saved });
      await response.promise;
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.workflowDefinition(saved.workflow.id) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.allWorkflows });
  } finally {
    vi.useRealTimers();
  }
});

it("exposes observation failure without restarting and retries only on request", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.subscribe).toHaveBeenCalledTimes(1);
  });
  const failure = new Error("Observation failed");
  act(() => {
    view.subscribe.mock.calls[0]?.[1].onError(failure);
    view.subscribe.mock.calls[0]?.[1].onOpen?.();
  });
  await waitFor(() => {
    expect(view.result.current.observation).toEqual(failure);
  });
  expect(view.subscribe).toHaveBeenCalledTimes(1);
  act(() => {
    view.result.current.retryWorkflowObservation();
  });
  await waitFor(() => {
    expect(view.subscribe).toHaveBeenCalledTimes(2);
  });
});

it("surfaces registration failure without starting a replacement observation", async () => {
  const failure = new Error("Registration failed");
  const view = setup(({ services }) => {
    vi.spyOn(services.api, "subscribeWorkflow").mockImplementation(() => {
      throw failure;
    });
  });
  await waitFor(() => {
    expect(view.result.current.observation?.cause).toBe(failure);
  });
  expect(view.subscribe).toHaveBeenCalledTimes(1);
  expect(view.close).not.toHaveBeenCalled();
});

it("releases its subscription when the destination owner is disposed", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.subscribe).toHaveBeenCalledTimes(1);
  });
  vi.useFakeTimers();
  try {
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    expect(view.close).toHaveBeenCalledTimes(1);
    expect(view.subscribe).toHaveBeenCalledTimes(1);
  } finally {
    vi.useRealTimers();
  }
});

it("does not refresh or retain observation when registration reports open after disposal", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.subscribe).toHaveBeenCalledTimes(1);
  });
  const handler = view.subscribe.mock.calls[0]?.[1];
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  vi.useFakeTimers();
  try {
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    act(() => handler?.onOpen?.());
    await act(async () => vi.advanceTimersByTimeAsync(500));
    expect(view.close).toHaveBeenCalledTimes(1);
    expect(invalidate).not.toHaveBeenCalled();
    expect(view.subscribe).toHaveBeenCalledTimes(1);
  } finally {
    vi.useRealTimers();
  }
});
