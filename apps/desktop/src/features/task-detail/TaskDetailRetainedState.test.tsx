import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { appI18n } from "@/i18n";
import type { SidebarDestination } from "@/app-facade";
import { createTestSidebarController, createTestSidebarNavigator } from "@/test-support/sidebar";
import {
  commentListResponse,
  emptyTaskAttentionResponse,
  mountTaskDetailSurface,
  questionAttentionResponse,
  taskGetRoute,
  taskCommentRoute,
  taskCommentPage,
  taskActivityRoute,
  taskActivityPage,
  taskIdentityFixture,
  taskBlockedByFixture,
  taskDetailResponse,
} from "@/test-support/task-detail";

describe("Task Detail retained sidebar state", () => {
  it("restores the selected Activity feed but opens back at the top after visiting another Task", async () => {
    const pageNavigator = createTestSidebarNavigator();
    const taskB = taskIdentityFixture(taskDetailResponse, "task-2", "Dependency target", "T-2");
    const services = mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      comments: commentListResponse,
      navigator: pageNavigator,
      routes: [
        taskGetRoute((taskID) => (taskID === "task-2" ? taskB : taskDetailResponse)),
        taskCommentRoute((taskID) => (taskID === "task-2" ? taskCommentPage([]) : commentListResponse)),
        taskActivityRoute((taskID) => taskActivityPage(taskID, 50)),
      ],
    });
    const user = userEvent.setup();

    const activityTab = await screen.findByRole("tab", { name: appI18n.t("task.activity") });
    await user.click(activityTab);
    await waitFor(() => {
      expect(activityTab).toHaveAttribute("aria-selected", "true");
    });
    const list = await screen.findByTestId("task-detail-island-stack");
    list.scrollTop = 1200;
    fireEvent.scroll(list);

    await waitFor(() => {
      expect(pageNavigator.registerCapture).toHaveBeenCalled();
    });
    const capture = vi.mocked(pageNavigator.registerCapture).mock.lastCall?.[0];
    if (capture === undefined) throw new Error("Expected Task Detail retained-state capture.");
    const retainedState = capture();
    expect(retainedState).toEqual(
      expect.objectContaining({
        selectedTab: "activity",
      }),
    );
    expect(retainedState).not.toHaveProperty("scrollOffsetPx");

    services.rerenderTaskDetail("task-2");
    await screen.findByDisplayValue("Dependency target");

    services.rerenderTaskDetail("task-1", retainedState);
    const restoredActivityTab = await screen.findByRole("tab", {
      name: appI18n.t("task.activity"),
    });
    await waitFor(() => {
      expect(restoredActivityTab).toHaveAttribute("aria-selected", "true");
      expect(screen.getByTestId("task-detail-island-stack").scrollTop).toBe(0);
    });
  });

  it("lets an attention deep-link override retained Task Detail state", async () => {
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });

    mountTaskDetailSurface(taskDetailResponse, {
      attention: questionAttentionResponse,
      initialFocus: { kind: "question", askIDs: ["ask-1"] },
      retainedState: {
        base: { body: "Need operator input", title: "Resolve blocker" },
        descriptionPresentation: { editing: false, expanded: false },
        draft: { body: "Need operator input", title: "Resolve blocker" },
        editingComment: null,
        newCommentBody: "",
        selectedTab: "comments",
      },
    });

    await screen.findByText("Choose snack");
    await waitFor(() => {
      expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "auto", block: "start" });
    });
  });

  it("layers retained unsaved interface state over refreshed Task data before capture", async () => {
    const pageNavigator = createTestSidebarNavigator();
    const retainedState = {
      base: { body: "Need operator input", title: "Resolve blocker" },
      descriptionPresentation: { editing: false, expanded: true },
      draft: { body: "Unsaved body", title: "Unsaved title" },
      editingComment: { body: "Unsaved edited comment", id: "comment-1" },
      newCommentBody: "Unsaved new comment",
      questionSelections: { "ask-1": ["not-retained"] },
      selectedTab: "comments",
    };

    mountTaskDetailSurface(taskDetailResponse, {
      comments: commentListResponse,
      attention: emptyTaskAttentionResponse,
      initialFocus: { kind: "dependencies" },
      navigator: pageNavigator,
      retainedState,
    });

    expect(pageNavigator.registerCapture).not.toHaveBeenCalled();
    expect(await screen.findByDisplayValue("Unsaved title")).toBeInTheDocument();

    await waitFor(() => {
      expect(pageNavigator.registerCapture).toHaveBeenCalled();
    });
    const calls = vi.mocked(pageNavigator.registerCapture).mock.calls;
    const latest = calls.at(-1)?.[0];
    if (latest === undefined) throw new Error("Expected Task Detail retained-state capture.");
    expect(latest()).toEqual(
      expect.objectContaining({
        base: { body: "Need operator input", title: "Resolve blocker" },
        descriptionPresentation: { editing: false, expanded: true },
        draft: { body: "Unsaved body", title: "Unsaved title" },
        editingComment: { body: "Unsaved edited comment", id: "comment-1" },
        newCommentBody: "Unsaved new comment",
        selectedTab: "comments",
      }),
    );
    expect(latest()).not.toHaveProperty("questionSelections");
  });

  it("lets refreshed server data replace a previously clean draft", async () => {
    mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      retainedState: {
        base: { body: "Old body", title: "Old title" },
        descriptionPresentation: { editing: false, expanded: false },
        draft: { body: "Old body", title: "Old title" },
        editingComment: null,
        newCommentBody: "",
        selectedTab: "comments",
      },
    });

    expect(await screen.findByDisplayValue("Resolve blocker")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("Old title")).not.toBeInTheDocument();
  });

  it("preserves overlay composition without carrying the current Task callback to a dependency", async () => {
    const pageNavigator = createTestSidebarNavigator();
    const onMutated = vi.fn();
    mountTaskDetailSurface(taskBlockedByFixture(taskDetailResponse), {
      navigator: pageNavigator,
      onMutated,
      sidebarMode: "overlay",
    });
    const user = userEvent.setup();

    await user.click(await screen.findByTestId("dependency-row-task-2"));
    expect(pageNavigator.push).toHaveBeenCalledWith({
      kind: "taskDetail",
      mode: "overlay",
      taskID: "task-2",
    });
  });

  it("ignores malformed retained state and preserves first-open focus", async () => {
    const navigator = createTestSidebarNavigator();
    mountTaskDetailSurface(taskDetailResponse, {
      initialFocus: { kind: "dependencies" },
      navigator,
      retainedState: { selectedTab: "unknown" },
    });
    expect(await screen.findByDisplayValue("Resolve blocker")).toBeInTheDocument();
    await waitFor(() => {
      expect(navigator.registerCapture).toHaveBeenCalled();
    });
    const capture = vi.mocked(navigator.registerCapture).mock.lastCall?.[0];
    if (capture === undefined) throw new Error("Expected fallback-state capture.");
    expect(capture()).toEqual(
      expect.objectContaining({
        draft: { body: "Need operator input", title: "Resolve blocker" },
        selectedTab: "comments",
      }),
    );
  });

  it("opens related creation through the standalone Task Detail root owner", async () => {
    const opened: SidebarDestination[] = [];
    const root = createTestSidebarController((destination) => {
      opened.push(destination);
    });
    mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      openSidebar: root.open,
    });
    const user = userEvent.setup();

    await user.click(await screen.findByTestId("dependency-add-blocked-by"));
    await user.click(await screen.findByRole("button", { name: appI18n.t("task.dependenciesCreateTask") }));

    expect(opened).toMatchObject([
      {
        initialPreparedDependency: {
          direction: "blocks",
          shortID: "T-1",
          status: { kind: "running" },
          taskID: "task-1",
          title: "Resolve blocker",
          workflowID: "11111111-1111-4111-8111-111111111111",
        },
        kind: "newTask",
      },
    ]);
  });
});
