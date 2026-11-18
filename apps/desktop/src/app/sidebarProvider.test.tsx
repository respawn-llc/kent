import { act, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import { StrictMode, useEffect, useLayoutEffect, useState, type ReactNode } from "react";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import { RegistryProvider } from "@effect/atom-react";
import { describe, expect, it, vi } from "bun:test";
import userEvent from "@testing-library/user-event";

import {
  useSidebarRoots,
  useSidebarShell,
  type SidebarBackResult,
  type SidebarDestination,
  type SidebarDestinationPolicy,
  type SidebarPageNavigator,
  type SidebarRootController,
  type TaskDetailInitialFocus,
} from "@/app-facade";
import { appI18n } from "@/i18n";
import { TestAppProviders } from "@/test-support/app-services";
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import {
  createTaskDetailTestServices,
  emptyTaskAttentionResponse,
  taskAttentionRoute,
  taskAttentionResponse,
  taskCommentPage,
  taskCommentRoute,
  taskDetailResponse,
  taskGetRoute,
  taskIdentityFixture,
} from "@/test-support/task-detail";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { SidebarProvider } from "./sidebarProvider";
import { useSidebarCurrentPage } from "./sidebarPageContext";
import { SidebarHost } from "./sidebar";

const policy: SidebarDestinationPolicy = {
  applyBackResult: (_destination, state, result) => ({ state, result }),
  equals: (left, right) => left.kind === "custom" && right.kind === "custom" && left.title === right.title,
  retainedState: (_destination, state) => state,
};

const emptyContent = () => null;

it.each([
  [640, false],
  [1100, false],
  [640, true],
  [1100, true],
] as const)("chooses contextual presentation from the %ipx owner pane (nested: %s)", (width, nested) => {
  const pane = document.createElement("section");
  const container = document.createElement("main");
  container.append(pane);
  if (nested) pane.setAttribute("data-sidebar-protected-main", "");
  const mainPaneRef = { current: nested ? container : pane };
  const measure = vi.spyOn(pane, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, width, 600));
  function adaptiveWrapper({ children }: Readonly<{ children: ReactNode }>) {
    return (
      <RegistryProvider>
        <SidebarProvider policy={policy} mainPaneRef={mainPaneRef}>
          {children}
        </SidebarProvider>
      </RegistryProvider>
    );
  }
  const { result } = renderHook(() => ({ roots: useSidebarRoots(), shell: useSidebarShell() }), {
    wrapper: adaptiveWrapper,
  });
  act(() => {
    result.current.roots.open({ ...destination("Goal"), sizing: { desiredWidthPx: 560, minWidthPx: 400 } });
  });
  const mode = width < 968 ? "overlay" : "shift";
  expect(result.current.shell.activeDestination?.mode).toBe(mode);
  measure.mockReturnValue(new DOMRect(0, 0, 420, 600));
  act(() => {
    result.current.roots.open(destination("Processes"));
  });
  expect(result.current.shell.activeDestination?.mode).toBe(mode);
  vi.useFakeTimers();
  act(() => {
    result.current.shell.close();
    vi.runAllTimers();
    result.current.roots.open(destination("Reopened"));
  });
  vi.useRealTimers();
  expect(result.current.shell.activeDestination?.mode).toBe("overlay");
});

function destination(title: string): Extract<SidebarDestination, { kind: "custom" }> {
  return { kind: "custom", title, content: emptyContent };
}

function wrapper({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <RegistryProvider>
      <SidebarProvider policy={policy}>{children}</SidebarProvider>
    </RegistryProvider>
  );
}

function strictWrapper({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <StrictMode>
      <RegistryProvider>
        <SidebarProvider policy={policy}>{children}</SidebarProvider>
      </RegistryProvider>
    </StrictMode>
  );
}

function productionWrapper({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <RegistryProvider>
      <SidebarProvider policy={sidebarDestinationPolicy}>{children}</SidebarProvider>
    </RegistryProvider>
  );
}

const newTaskDestination = {
  boardQueryWorkflowID: "workflow-1",
  kind: "newTask",
  projectID: "project-1",
  workflowID: "workflow-1",
} as const;
const newTaskDraft = {
  formValues: {
    body: "Parent body",
    sourceURL: "original ticket text",
    sourceWorkspaceID: "workspace-1",
    title: "Parent",
  },
  preparedDependencies: [],
  selectedLabelIDs: ["label-1"],
};
const childResult = {
  kind: "newTaskCreated",
  direction: "blocked-by",
  task: {
    id: "task-child",
    shortID: "KENT-2",
    status: { kind: "backlog", nativeState: "active", nodeIDs: [], attentionTypes: [] },
    title: "Child",
    workflowID: "workflow-1",
  },
} as const satisfies SidebarBackResult;

function useHarness() {
  return {
    page: useSidebarCurrentPage(),
    roots: useSidebarRoots(),
    shell: useSidebarShell(),
  };
}

function requireNavigator(value: ReturnType<typeof useHarness>): SidebarPageNavigator {
  const navigator = value.page?.navigator;
  if (navigator === undefined) {
    throw new Error("Expected a mounted sidebar page.");
  }
  return navigator;
}

function installAttentionTargetScrollSpy() {
  const focusTargets: HTMLElement[] = [];
  const scrollIntoView = vi.fn(function (this: HTMLElement) {
    focusTargets.push(this);
  });
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value: scrollIntoView,
  });
  return {
    focusTargets,
    scrollIntoView,
    restore() {
      if (original === undefined) {
        Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
      } else {
        Object.defineProperty(HTMLElement.prototype, "scrollIntoView", original);
      }
    },
  };
}

function mountSidebarTaskDetail(initialFocus: TaskDetailInitialFocus | undefined, strictMode = false) {
  const otherTask = taskIdentityFixture(taskDetailResponse, "task-2", "Dependency target", "T-2");
  const comments = taskCommentPage(
    Array.from({ length: 20 }, (_value, index) => ({
      id: `comment-${String(index)}`,
      body: "Comment",
      milliseconds: index,
    })),
  );
  const services = createTaskDetailTestServices(taskDetailResponse, {
    attention: taskAttentionResponse,
    comments,
    routes: [
      taskGetRoute((taskID) => (taskID === "task-2" ? otherTask : taskDetailResponse)),
      taskAttentionRoute((taskID) =>
        taskID === "task-2" ? emptyTaskAttentionResponse : taskAttentionResponse,
      ),
      taskCommentRoute((taskID) => (taskID === "task-2" ? taskCommentPage([]) : comments)),
    ],
  });
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: ["/"] }),
    routeTree: createRootRoute(),
  });
  const destination = {
    kind: "taskDetail",
    taskID: "task-1",
    ...(initialFocus === undefined ? {} : { initialFocus }),
  } satisfies SidebarDestination;
  let rootController: SidebarRootController | undefined;

  function NavigationControls() {
    const roots = useSidebarRoots();
    const page = useSidebarCurrentPage();
    useLayoutEffect(() => {
      rootController = roots;
    }, [roots]);

    return (
      <>
        <button
          data-testid="open-other-task"
          onClick={() => {
            page?.navigator.push({ kind: "taskDetail", taskID: "task-2" });
          }}
          type="button"
        />
        <button
          data-testid="open-task-ordinarily"
          onClick={() => {
            roots.open({ kind: "taskDetail", taskID: "task-1" });
          }}
          type="button"
        />
        <button
          data-testid="focus-question"
          onClick={() => {
            page?.navigator.replace({
              kind: "taskDetail",
              initialFocus: { kind: "question", askIDs: ["ask-1"] },
              taskID: "task-1",
            });
          }}
          type="button"
        />
        <button
          data-testid="focus-dependencies"
          onClick={() => {
            page?.navigator.replace({
              kind: "taskDetail",
              initialFocus: { kind: "dependencies" },
              taskID: "task-1",
            });
          }}
          type="button"
        />
        <button
          data-testid="open-question-target"
          onClick={() => {
            roots.open({
              kind: "taskDetail",
              initialFocus: { kind: "question", askIDs: ["ask-1"] },
              taskID: "task-1",
            });
          }}
          type="button"
        />
        <button
          data-testid="open-approval-target"
          onClick={() => {
            roots.open({
              kind: "taskDetail",
              initialFocus: { kind: "approval", approvalID: "approval-1" },
              taskID: "task-1",
            });
          }}
          type="button"
        />
        <button
          data-testid="open-original-task"
          onClick={() => {
            page?.navigator.push({ kind: "taskDetail", taskID: "task-1" });
          }}
          type="button"
        />
        <button
          data-testid="return-to-task"
          onClick={() => {
            page?.navigator.back();
          }}
          type="button"
        />
      </>
    );
  }

  const content = (
    <RouterContextProvider router={router}>
      <TestAppProviders services={services}>
        <SidebarProvider policy={sidebarDestinationPolicy}>
          <NavigationControls />
          <SidebarHost />
        </SidebarProvider>
      </TestAppProviders>
    </RouterContextProvider>
  );
  const view = render(strictMode ? <StrictMode>{content}</StrictMode> : content);
  const roots = rootController;
  if (roots === undefined) throw new Error("Expected sidebar root controls.");
  act(() => {
    roots.open(destination);
  });
  return { unmount: view.unmount };
}

function ShellHarness() {
  const roots = useSidebarRoots();
  const page = useSidebarCurrentPage();
  const [available, setAvailable] = useState(true);

  useLayoutEffect(() => {
    const root = roots.open({
      kind: "custom",
      title: "A",
      content: () => <div data-testid="page-a" />,
    });
    return root.release;
  }, [roots]);

  useEffect(() => {
    if (page === null) {
      return;
    }
    const detachCapture = page.navigator.registerCapture(() => ({ page: "A" }));
    const detachAvailability = page.navigator.registerAvailability({
      back: available,
      close: available,
    });
    return () => {
      detachAvailability();
      detachCapture();
    };
  }, [available, page]);

  return (
    <>
      <button
        data-testid="push-page"
        onClick={() => {
          page?.navigator.push({
            kind: "custom",
            title: "B",
            content: () => <div data-testid="page-b" />,
          });
        }}
        type="button"
      />
      <button
        data-testid="toggle-availability"
        onClick={() => {
          setAvailable((current) => !current);
        }}
        type="button"
      />
      <SidebarHost />
    </>
  );
}

describe("SidebarProvider stack", () => {
  it("keeps the current Task root and transfers its lifecycle on an ordinary reopen", async () => {
    const { result } = renderHook(useHarness, { wrapper: productionWrapper });
    let original: ReturnType<typeof result.current.roots.open> | undefined;
    let reopened: ReturnType<typeof result.current.roots.open> | undefined;

    act(() => {
      original = result.current.roots.open({ kind: "taskDetail", taskID: "task-1" });
      reopened = result.current.roots.open({ kind: "taskDetail", taskID: "task-1" });
    });
    expect(result.current.shell.activeDestination).toMatchObject({ kind: "taskDetail", taskID: "task-1" });
    const originalHandle = original;
    const reopenedHandle = reopened;
    if (originalHandle === undefined || reopenedHandle === undefined)
      throw new Error("Expected Task root handles.");
    expect(reopenedHandle.lifecycle).toBe(originalHandle.lifecycle);

    act(() => {
      originalHandle.release();
    });
    expect(result.current.shell.activeDestination).toMatchObject({ kind: "taskDetail", taskID: "task-1" });

    act(() => {
      reopenedHandle.release();
    });
    expect(result.current.shell.activeDestination).toBeNull();
    expect(await reopenedHandle.lifecycle).toBe("released");
  });

  it("opens a history-restored Task at the top without replaying its earlier attention target", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const focus = installAttentionTargetScrollSpy();
    const view = mountSidebarTaskDetail({ kind: "question", askIDs: ["ask-1"] });
    try {
      const user = userEvent.setup();
      const questionLabel = appI18n.t("task.question");
      await screen.findByLabelText(questionLabel);
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });

      await user.click(screen.getByTestId("open-other-task"));
      await screen.findByRole("button", { name: appI18n.t("app.back") });
      await waitFor(() => {
        expect(screen.queryByLabelText(questionLabel)).not.toBeInTheDocument();
      });
      await user.click(screen.getByTestId("return-to-task"));

      await screen.findByLabelText(questionLabel);
      await waitFor(() => {
        expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(0);
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });
    } finally {
      view.unmount();
      geometry.restore();
      focus.restore();
    }
  });

  it("does not replay old focus when an ordinary Task link restores a prior sidebar entry", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const focus = installAttentionTargetScrollSpy();
    const view = mountSidebarTaskDetail({ kind: "question", askIDs: ["ask-1"] });
    try {
      const user = userEvent.setup();
      const questionLabel = appI18n.t("task.question");
      await screen.findByLabelText(questionLabel);
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });

      await user.click(screen.getByTestId("open-other-task"));
      await screen.findByRole("button", { name: appI18n.t("app.back") });
      await user.click(screen.getByTestId("open-original-task"));

      await screen.findByLabelText(questionLabel);
      await waitFor(() => {
        expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(0);
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });
    } finally {
      view.unmount();
      geometry.restore();
      focus.restore();
    }
  });

  it("opens a Task with pending attention at the top after navigating to a different Task", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const focus = installAttentionTargetScrollSpy();
    const view = mountSidebarTaskDetail(undefined);
    try {
      const user = userEvent.setup();
      const questionLabel = appI18n.t("task.question");
      await screen.findByLabelText(questionLabel);
      const list = screen.getByTestId("task-detail-island-stack");
      list.scrollTop = 1200;
      fireEvent.scroll(list);
      expect(list.scrollTop).toBeGreaterThan(0);

      await user.click(screen.getByTestId("open-other-task"));
      await screen.findByRole("button", { name: appI18n.t("app.back") });
      await waitFor(() => {
        expect(screen.queryByLabelText(questionLabel)).not.toBeInTheDocument();
        expect(focus.scrollIntoView).not.toHaveBeenCalled();
      });

      await user.click(screen.getByTestId("return-to-task"));
      await screen.findByLabelText(questionLabel);
      await waitFor(() => {
        expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(0);
        expect(focus.scrollIntoView).not.toHaveBeenCalled();
      });
    } finally {
      view.unmount();
      geometry.restore();
      focus.restore();
    }
  });

  it("leaves scrolling unchanged when an ordinary action opens the already-open Task", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const view = mountSidebarTaskDetail(undefined);
    try {
      const user = userEvent.setup();
      await screen.findByLabelText(appI18n.t("task.question"));
      const list = screen.getByTestId("task-detail-island-stack");
      list.scrollTop = 1200;
      fireEvent.scroll(list);
      const scrollOffset = list.scrollTop;
      expect(scrollOffset).toBeGreaterThan(0);

      await user.click(screen.getByTestId("open-task-ordinarily"));
      expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(scrollOffset);
    } finally {
      view.unmount();
      geometry.restore();
    }
  });

  it("repeats explicit Question navigation through root open and sidebar replace", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const focus = installAttentionTargetScrollSpy();
    const view = mountSidebarTaskDetail(undefined, true);
    try {
      const user = userEvent.setup();
      const questionLabel = appI18n.t("task.question");
      const approvalLabel = appI18n.t("task.approval");
      await screen.findByLabelText(questionLabel);

      await user.click(screen.getByTestId("open-question-target"));
      const question = await screen.findByLabelText(questionLabel);
      const approval = screen.getByLabelText(approvalLabel);
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });
      expect(focus.focusTargets[0]?.contains(question)).toBe(true);
      expect(focus.focusTargets[0]?.contains(approval)).toBe(false);

      const firstList = screen.getByTestId("task-detail-island-stack");
      firstList.scrollTop = 1200;
      fireEvent.scroll(firstList);
      await user.click(screen.getByTestId("open-question-target"));
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(2);
      });
      expect(focus.focusTargets[1]?.contains(screen.getByLabelText(questionLabel))).toBe(true);
      expect(focus.focusTargets[1]?.contains(screen.getByLabelText(approvalLabel))).toBe(false);

      const secondList = screen.getByTestId("task-detail-island-stack");
      secondList.scrollTop = 1200;
      fireEvent.scroll(secondList);
      await user.click(screen.getByTestId("focus-question"));
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(3);
      });
      expect(focus.focusTargets[2]?.contains(screen.getByLabelText(questionLabel))).toBe(true);
      expect(focus.focusTargets[2]?.contains(screen.getByLabelText(approvalLabel))).toBe(false);
    } finally {
      view.unmount();
      geometry.restore();
      focus.restore();
    }
  });

  it.each([
    ["question", { kind: "question", askIDs: ["ask-1"] }, "task.question", "task.approval"],
    ["approval", { kind: "approval", approvalID: "approval-1" }, "task.approval", "task.question"],
  ] as const)(
    "opens the requested %s target, then returns to the top on a later ordinary opening",
    async (_target, initialFocus, requestedKey, otherKey) => {
      const geometry = installVirtualizedScrollGeometry(600);
      const focus = installAttentionTargetScrollSpy();
      const view = mountSidebarTaskDetail(initialFocus, true);
      try {
        const user = userEvent.setup();
        const requestedLabel = appI18n.t(requestedKey);
        const otherLabel = appI18n.t(otherKey);
        const requestedTarget = await screen.findByLabelText(requestedLabel);
        const otherTarget = screen.getByLabelText(otherLabel);
        await waitFor(() => {
          expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
        });
        expect(focus.focusTargets[0]?.contains(requestedTarget)).toBe(true);
        expect(focus.focusTargets[0]?.contains(otherTarget)).toBe(false);

        await user.click(screen.getByTestId("open-other-task"));
        await screen.findByRole("button", { name: appI18n.t("app.back") });
        await user.click(screen.getByTestId("open-task-ordinarily"));

        await screen.findByLabelText(requestedLabel);
        await waitFor(() => {
          expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(0);
          expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
        });
      } finally {
        view.unmount();
        geometry.restore();
        focus.restore();
      }
    },
  );

  it("repeats an explicit Approval jump after scrolling away from its target", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const focus = installAttentionTargetScrollSpy();
    const view = mountSidebarTaskDetail(undefined, true);
    try {
      const user = userEvent.setup();
      const approvalLabel = appI18n.t("task.approval");
      const questionLabel = appI18n.t("task.question");
      await screen.findByLabelText(questionLabel);

      await user.click(screen.getByTestId("open-approval-target"));
      const approval = await screen.findByLabelText(approvalLabel);
      const question = screen.getByLabelText(questionLabel);
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(1);
      });
      expect(focus.focusTargets[0]?.contains(approval)).toBe(true);
      expect(focus.focusTargets[0]?.contains(question)).toBe(false);

      const list = screen.getByTestId("task-detail-island-stack");
      list.scrollTop = 1200;
      fireEvent.scroll(list);
      await user.click(screen.getByTestId("open-approval-target"));
      await waitFor(() => {
        expect(focus.scrollIntoView).toHaveBeenCalledTimes(2);
      });
      expect(focus.focusTargets[1]?.contains(screen.getByLabelText(approvalLabel))).toBe(true);
      expect(focus.focusTargets[1]?.contains(screen.getByLabelText(questionLabel))).toBe(false);
    } finally {
      view.unmount();
      geometry.restore();
      focus.restore();
    }
  });

  it("repeats explicit dependency navigation after scrolling away from that section", async () => {
    const geometry = installVirtualizedScrollGeometry(600);
    const view = mountSidebarTaskDetail(undefined);
    try {
      const user = userEvent.setup();
      await screen.findByLabelText(appI18n.t("task.question"));
      const initialList = screen.getByTestId("task-detail-island-stack");
      initialList.scrollTop = 1200;
      fireEvent.scroll(initialList);
      const scrolledOffset = initialList.scrollTop;
      expect(scrolledOffset).toBeGreaterThan(0);

      await user.click(screen.getByTestId("focus-dependencies"));
      const dependenciesHeading = await screen.findByRole("heading", {
        name: appI18n.t("task.dependencies"),
      });
      const list = screen.getByTestId("task-detail-island-stack");
      await waitFor(() => {
        expect(list.scrollTop).toBeLessThan(scrolledOffset);
      });
      expect(dependenciesHeading).toBeInTheDocument();

      list.scrollTop = 1200;
      fireEvent.scroll(list);
      const repeatedScrolledOffset = list.scrollTop;
      await user.click(screen.getByTestId("focus-dependencies"));
      await waitFor(() => {
        expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBeLessThan(repeatedScrolledOffset);
      });
      expect(screen.getByRole("heading", { name: appI18n.t("task.dependencies") })).toBeInTheDocument();
    } finally {
      view.unmount();
      geometry.restore();
    }
  });

  it("toggles the current Worktree root and replaces other roots without history", () => {
    const { result } = renderHook(useHarness, { wrapper: productionWrapper });
    const worktree = {
      kind: "worktree",
      projectID: "project-1",
      sessionID: "session-1",
      page: "list",
      openSession: vi.fn(),
    } as const;
    act(() => {
      result.current.roots.open(worktree);
    });
    act(() => {
      expect(result.current.roots.toggle({ ...worktree })).toBeNull();
    });
    expect(result.current.shell.phase).toBe("closing");
    act(() => {
      result.current.roots.toggle(worktree);
    });
    expect(result.current.shell.phase).toBe("open");
    act(() => {
      result.current.roots.toggle({ kind: "processes", projectID: "project-1", sessionID: "session-1" });
    });
    expect(result.current.shell.activeDestination?.kind).toBe("processes");
    expect(result.current.shell.canGoBack).toBe(false);
  });

  it("toggles a custom root by identity and respects its close availability", () => {
    const { result } = renderHook(useHarness, { wrapper: productionWrapper });
    const goal = { kind: "custom", title: "Goal", content: emptyContent, identity: Symbol("goal") } as const;
    act(() => {
      result.current.roots.toggle(goal);
    });
    const page = requireNavigator(result.current);
    let unregister: (() => void) | undefined;
    act(() => {
      unregister = page.registerAvailability({ back: true, close: false });
      expect(result.current.roots.toggle({ ...goal, content: () => null })).toBeNull();
    });
    expect(result.current.shell.phase).toBe("open");
    act(() => {
      unregister?.();
      expect(result.current.roots.toggle({ ...goal, content: () => null })).toBeNull();
    });
    expect(result.current.shell.phase).toBe("closing");
  });

  it("pushes, restores snapshots, truncates a matching branch, replaces, and goes Back", async () => {
    const { result } = renderHook(useHarness, { wrapper });
    let lifecycle: Promise<unknown> | undefined;

    act(() => {
      lifecycle = result.current.roots.open(destination("A")).lifecycle;
    });
    const a = requireNavigator(result.current);
    const captured: string[] = [];
    act(() => {
      a.registerCapture(() => {
        captured.push("A");
        return { draft: "a" };
      });
      expect(a.push(destination("B"))).toBe("accepted");
      expect(a.back()).toBe("stale");
    });
    expect(captured).toEqual(["A"]);
    expect(result.current.shell.activeDestination).toMatchObject(destination("B"));

    const b = requireNavigator(result.current);
    act(() => {
      b.registerCapture(() => ({ draft: "b" }));
      expect(b.push(destination("C"))).toBe("accepted");
    });
    const c = requireNavigator(result.current);
    act(() => {
      expect(c.push(destination("A"))).toBe("accepted");
    });
    expect(result.current.shell.activeDestination).toMatchObject(destination("A"));
    expect(result.current.page?.retainedState).toEqual({ draft: "a" });

    const restoredA = requireNavigator(result.current);
    act(() => {
      expect(restoredA.replace(destination("D"))).toBe("accepted");
    });
    const d = requireNavigator(result.current);
    act(() => {
      expect(d.back()).toBe("accepted");
    });
    expect(result.current.shell.phase).toBe("closing");
    expect(lifecycle).resolves.toBe("closed");
  });

  it("rejects append Push without capture while leaving the page capability active", () => {
    const { result } = renderHook(useHarness, { wrapper });

    act(() => {
      result.current.roots.open(destination("A"));
    });
    const navigator = requireNavigator(result.current);
    act(() => {
      expect(navigator.push(destination("B"))).toBe("unavailable");
      expect(navigator.back()).toBe("accepted");
    });
    expect(result.current.shell.activeDestination).toMatchObject(destination("A"));
    expect(result.current.shell.phase).toBe("closing");
  });

  it("does not capture discarded pages and revokes before an accepted state update", () => {
    const { result } = renderHook(useHarness, { wrapper });

    act(() => {
      result.current.roots.open(destination("A"));
    });
    const a = requireNavigator(result.current);
    const capture = vi.fn(() => ({ draft: "a" }));
    act(() => {
      a.registerCapture(capture);
      expect(a.replace(destination("B"))).toBe("accepted");
      expect(a.close()).toBe("stale");
    });
    expect(capture).not.toHaveBeenCalled();
    expect(result.current.shell.activeDestination).toMatchObject(destination("B"));

    const b = requireNavigator(result.current);
    act(() => {
      b.registerCapture(capture);
      result.current.roots.open(destination("C"));
      expect(b.back()).toBe("stale");
    });
    expect(capture).not.toHaveBeenCalled();
    expect(result.current.shell.activeDestination).toMatchObject(destination("C"));
  });

  it("keeps the root and the newest non-root pages within the 50-entry limit", () => {
    const { result } = renderHook(useHarness, { wrapper });

    act(() => {
      result.current.roots.open(destination("0"));
    });
    for (let index = 1; index <= 51; index += 1) {
      const navigator = requireNavigator(result.current);
      act(() => {
        navigator.registerCapture(() => ({ index: index - 1 }));
        expect(navigator.push(destination(index.toString()))).toBe("accepted");
      });
    }

    const visited: string[] = [];
    while (result.current.shell.canGoBack) {
      const current = result.current.shell.activeDestination;
      if (current?.kind === "custom") {
        visited.push(current.title);
      }
      act(() => {
        expect(requireNavigator(result.current).back()).toBe("accepted");
      });
    }
    expect(visited).toHaveLength(49);
    expect(visited).not.toContain("1");
    expect(result.current.shell.activeDestination).toMatchObject(destination("0"));
  });

  it("keeps a replayed current-page registration usable without reactivating stale pages", () => {
    const { result } = renderHook(useHarness, { wrapper: strictWrapper });

    act(() => {
      result.current.roots.open(destination("A"));
    });
    const a = requireNavigator(result.current);
    act(() => {
      a.registerCapture(() => ({ draft: "a" }));
      expect(a.push(destination("B"))).toBe("accepted");
    });
    act(() => {
      expect(a.back()).toBe("stale");
      expect(requireNavigator(result.current).back()).toBe("accepted");
    });
    expect(result.current.shell.activeDestination).toMatchObject(destination("A"));
  });

  it("settles only the owning root for replacement, release, close, and provider teardown", async () => {
    const view = renderHook(useHarness, { wrapper });
    let firstRoot: ReturnType<typeof view.result.current.roots.open> | undefined;
    let secondRoot: ReturnType<typeof view.result.current.roots.open> | undefined;
    act(() => {
      firstRoot = view.result.current.roots.open(destination("A"));
    });
    const oldPage = requireNavigator(view.result.current);
    let staleClose: ReturnType<typeof oldPage.close> | undefined;
    act(() => {
      secondRoot = view.result.current.roots.open(destination("B"));
      staleClose = oldPage.close();
      firstRoot?.release();
    });
    expect(firstRoot?.lifecycle).resolves.toBe("replaced");
    expect(staleClose).toBe("stale");
    expect(view.result.current.shell.activeDestination).toMatchObject(destination("B"));
    act(() => {
      secondRoot?.release();
    });
    expect(secondRoot?.lifecycle).resolves.toBe("released");
    expect(view.result.current.shell.activeDestination).toBeNull();

    let teardownRoot: ReturnType<typeof view.result.current.roots.open> | undefined;
    act(() => {
      teardownRoot = view.result.current.roots.open(destination("C"));
    });
    const teardownPage = requireNavigator(view.result.current);
    view.unmount();
    expect(teardownRoot?.lifecycle).resolves.toBe("released");
    expect(teardownPage.close()).toBe("stale");
    teardownRoot?.release();
  });

  it("keeps the active sidebar mode across root replacement and chooses again after close", () => {
    vi.useFakeTimers();
    const { result } = renderHook(useHarness, { wrapper });

    act(() => {
      result.current.roots.open({ ...destination("A"), mode: "shift" });
      result.current.roots.open({ ...destination("B"), mode: "overlay" });
    });
    expect(result.current.shell.activeDestination).toEqual({ ...destination("B"), mode: "shift" });

    act(() => {
      expect(requireNavigator(result.current).close()).toBe("accepted");
      vi.runAllTimers();
      result.current.roots.open({ ...destination("C"), mode: "overlay" });
    });
    expect(result.current.shell.activeDestination).toEqual({ ...destination("C"), mode: "overlay" });
    vi.useRealTimers();
  });

  it("replays capture and availability registration without clearing a newer registration", () => {
    const { result } = renderHook(useHarness, { wrapper });
    act(() => {
      result.current.roots.open(destination("A"));
    });
    const a = requireNavigator(result.current);
    const firstCapture = vi.fn(() => ({ draft: "old" }));
    const secondCapture = vi.fn(() => ({ draft: "new" }));
    act(() => {
      const detachFirst = a.registerCapture(firstCapture);
      a.registerCapture(secondCapture);
      detachFirst();
      expect(a.push(destination("B"))).toBe("accepted");
    });
    expect(firstCapture).not.toHaveBeenCalled();
    expect(secondCapture).toHaveBeenCalledTimes(1);

    const b = requireNavigator(result.current);
    let detachAvailability: (() => void) | undefined;
    act(() => {
      detachAvailability = b.registerAvailability({ back: false, close: false });
    });
    expect(result.current.shell.canGoBack).toBe(true);
    expect(result.current.shell.backAvailable).toBe(false);
    expect(result.current.shell.closeAvailable).toBe(false);
    expect(result.current.shell.back()).toBe("unavailable");
    act(() => {
      detachAvailability?.();
    });
    expect(result.current.shell.backAvailable).toBe(true);
    act(() => {
      expect(b.back()).toBe("accepted");
    });
    expect(result.current.page?.retainedState).toEqual({ draft: "new" });
  });

  it("integrates child results and retained Task branches through the production policy", () => {
    const { result } = renderHook(useHarness, { wrapper: productionWrapper });
    act(() => {
      result.current.roots.open({ kind: "taskDetail", taskID: "task-origin" });
    });
    const origin = requireNavigator(result.current);
    act(() => {
      origin.registerCapture(() => ({ origin: true }));
      expect(origin.push(newTaskDestination)).toBe("accepted");
    });
    act(() => {
      requireNavigator(result.current).registerCapture(() => newTaskDraft);
      expect(requireNavigator(result.current).push(newTaskDestination)).toBe("accepted");
    });
    act(() => {
      expect(requireNavigator(result.current).back(childResult)).toBe("accepted");
    });
    const stagedDraft = {
      ...newTaskDraft,
      preparedDependencies: [
        {
          direction: "blocked-by",
          taskID: "task-child",
          shortID: "KENT-2",
          status: childResult.task.status,
          title: "Child",
          workflowID: "workflow-1",
        },
      ],
    };
    expect(result.current.page?.retainedState).toEqual(stagedDraft);

    const capture = vi.fn(() => stagedDraft);
    act(() => {
      requireNavigator(result.current).registerCapture(capture);
      expect(requireNavigator(result.current).push({ kind: "taskDetail", taskID: "task-unrelated" })).toBe(
        "accepted",
      );
    });
    expect(capture).toHaveBeenCalledTimes(1);
    act(() => {
      expect(requireNavigator(result.current).back()).toBe("accepted");
    });
    expect(result.current.page?.retainedState).toEqual(stagedDraft);

    const discardedCapture = vi.fn(() => newTaskDraft);
    act(() => {
      requireNavigator(result.current).registerCapture(discardedCapture);
      expect(requireNavigator(result.current).push({ kind: "taskDetail", taskID: "task-origin" })).toBe(
        "accepted",
      );
    });
    expect(discardedCapture).not.toHaveBeenCalled();
    expect(result.current.shell.activeDestination).toMatchObject({
      kind: "taskDetail",
      taskID: "task-origin",
    });
    expect(result.current.shell.canGoBack).toBe(false);
  });

  it("retains widths per profile and keeps the closing page rendered through the exit phase", () => {
    vi.useFakeTimers();
    const { result } = renderHook(useHarness, { wrapper });
    act(() => {
      result.current.roots.open(destination("A"));
      result.current.shell.resize({ px: 430 });
    });
    const a = requireNavigator(result.current);
    act(() => {
      a.registerCapture(() => ({ draft: "a" }));
      expect(
        a.push({
          kind: "custom",
          title: "Wide",
          content: () => null,
          sizing: { desiredWidthPx: 700, minWidthPx: 400 },
        }),
      ).toBe("accepted");
    });
    expect(result.current.shell.sidebarWidthPx).toBe(700);
    act(() => {
      result.current.shell.resize({ px: 600 });
      expect(requireNavigator(result.current).back()).toBe("accepted");
    });
    expect(result.current.shell.sidebarWidthPx).toBe(430);
    act(() => {
      expect(requireNavigator(result.current).close()).toBe("accepted");
    });
    expect(result.current.shell.phase).toBe("closing");
    expect(result.current.shell.activeDestination).toMatchObject(destination("A"));
    act(() => {
      vi.runAllTimers();
    });
    expect(result.current.shell.activeDestination).toBeNull();
    vi.useRealTimers();
  });

  it("renders X before Back, hides root Back, and mounts only the current page", () => {
    render(
      <RegistryProvider>
        <SidebarProvider policy={policy}>
          <ShellHarness />
        </SidebarProvider>
      </RegistryProvider>,
    );
    const headerButtons = () =>
      within(screen.getByTestId("app-sidebar-leading-controls")).getAllByRole("button");
    expect(headerButtons()).toHaveLength(1);
    expect(screen.getByTestId("page-a")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("push-page"));
    expect(headerButtons()).toHaveLength(2);
    expect(screen.queryByTestId("page-a")).toBeNull();
    expect(screen.getByTestId("page-b")).toBeInTheDocument();
    expect(screen.getByTestId("app-sidebar-page").getAttribute("data-direction")).toBe("push");

    fireEvent.click(screen.getByTestId("toggle-availability"));
    expect(headerButtons()[0]?.hasAttribute("disabled")).toBe(true);
    expect(headerButtons()[1]?.hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByTestId("toggle-availability"));
    const backButton = headerButtons()[1];
    if (backButton === undefined) {
      throw new Error("Expected nested sidebar Back.");
    }
    fireEvent.click(backButton);

    expect(headerButtons()).toHaveLength(1);
    expect(screen.getByTestId("page-a")).toBeInTheDocument();
    expect(screen.queryByTestId("page-b")).toBeNull();
    expect(screen.getByTestId("app-sidebar-page").getAttribute("data-direction")).toBe("back");
  });
});
