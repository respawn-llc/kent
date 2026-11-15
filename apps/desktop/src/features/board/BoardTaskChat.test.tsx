import { expect, it, vi } from "bun:test";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { TaskDetail } from "@/api";
import { TestAppProviders } from "@/test-support/app-services";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";
import { deferred } from "@/test-support/chat-runtime";
import { TestSidebar } from "@/test-support/sidebar-rendering";
import { DragDropSurface } from "@app/ui-kit";
import { KanbanColumn } from "./BoardColumns";
import type { KanbanCardVM } from "./BoardColumnViewModel";
import { Popover, PopoverAnchor } from "@/ui";
import { TaskSessionPicker } from "@/shared/task-session-chat";
import { ChatPromptPresenceProvider } from "@/app-facade";
import { BoardTaskChatContext, useBoardTaskChat } from "./BoardTaskChat";

function ChatEntry() {
  const chat = useBoardTaskChat("task-1");
  return (
    <Popover open={chat.pickerOpen} onOpenChange={chat.setPickerOpen}>
      <PopoverAnchor asChild>
        <button aria-busy={chat.pending} onClick={chat.open}>
          Open
        </button>
      </PopoverAnchor>
      <TaskSessionPicker
        sessions={chat.sessions}
        defaultIntent="ordinary"
        nativeWindowsAvailable={false}
        pendingSessionID={null}
        onOpen={chat.select}
      />
    </Popover>
  );
}

const card: KanbanCardVM = {
  id: "task-1",
  shortID: "KNT-1",
  title: "Task",
  preview: { markdown: "", truncated: false },
  activeNodeIDs: ["node-1"],
  statusKind: "interrupted",
  actions: { canResume: true, canStart: false, canInterrupt: false, canDelete: true },
  labels: [],
  borderTone: "default",
  dependencyProgress: null,
  workspaceChipLabel: null,
  updatedAt: 1,
};

function mount(boardCard?: KanbanCardVM) {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const openSession = vi.fn<(sessionID: string) => Promise<void>>(async () => undefined);
  const report = vi.fn();
  const openDetail = vi.fn();
  render(
    <TestAppProviders services={services}>
      <ChatPromptPresenceProvider>
        <TestSidebar>
          <BoardTaskChatContext.Provider value={{ openSession, report }}>
            {boardCard === undefined ? (
              <ChatEntry />
            ) : (
              <DragDropSurface onDrop={vi.fn()} onCancel={vi.fn()}>
                <KanbanColumn
                  column={{ id: "node-1", name: "Doing", taskCount: 1, assigneeRole: "" }}
                  cards={[boardCard]}
                  dragDisabled
                  dropState="idle"
                  hasMoreCards={false}
                  isLoadingMoreCards={false}
                  isFirstActive
                  onCardClick={openDetail}
                  onCardDragStart={vi.fn()}
                  onDeleteTask={vi.fn()}
                  onInterruptTask={vi.fn()}
                  onResumeTask={vi.fn()}
                  onLoadMoreCards={vi.fn()}
                  onColumnDisclosure={vi.fn()}
                />
              </DragDropSurface>
            )}
          </BoardTaskChatContext.Provider>
        </TestSidebar>
      </ChatPromptPresenceProvider>
    </TestAppProviders>,
  );
  return { services, openSession, openDetail, report };
}

it.each([0, 1, 2])("opens only an existing selected Session with %s choices", async (count) => {
  const { services, openSession } = mount();
  const base = await services.api.getTask("task-1");
  const sessions = [
    { sessionID: "session-a", displayName: "First session" },
    { sessionID: "session-b", displayName: "Second session" },
  ].slice(0, count);
  const get = vi.spyOn(services.api, "getTask").mockResolvedValue({ ...base, liveSessions: sessions });
  const resume = vi.spyOn(services.api, "resumeTask");
  expect(get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  await waitFor(() => {
    expect(get).toHaveBeenCalledTimes(1);
  });
  if (count === 2) {
    expect(openSession).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Second session" }), { shiftKey: true });
  }
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Open" })).toHaveAttribute("aria-busy", "false"),
  );
  expect(openSession.mock.calls).toEqual(count === 0 ? [] : [[count === 1 ? "session-a" : "session-b"]]);
  expect(resume).not.toHaveBeenCalled();
});

it("shows an in-flight read, coalesces repeated activation, and permits retry after a read failure", async () => {
  const { services, openSession, report } = mount();
  const base = await services.api.getTask("task-1");
  const result = deferred<TaskDetail>();
  const get = vi
    .spyOn(services.api, "getTask")
    .mockReturnValueOnce(result.promise)
    .mockResolvedValue({
      ...base,
      liveSessions: [{ sessionID: "session-a", displayName: "First" }],
    });
  const button = screen.getByRole("button", { name: "Open" });
  fireEvent.click(button);
  await waitFor(() => expect(button).toHaveAttribute("aria-busy", "true"));
  fireEvent.click(button);
  expect(get).toHaveBeenCalledTimes(1);
  const error = new Error("offline");
  await act(async () => {
    result.reject(error);
  });
  await waitFor(() => {
    expect(report).toHaveBeenCalledWith(error);
  });
  expect(openSession).not.toHaveBeenCalled();
  fireEvent.click(button);
  await waitFor(() => {
    expect(openSession).toHaveBeenCalledWith("session-a");
  });
  expect(get).toHaveBeenCalledTimes(2);
});

it("keeps ordinary card clicks on Detail and Shift-clicks on existing Chat", async () => {
  const { services, openSession, openDetail } = mount(card);
  const base = await services.api.getTask("task-1");
  const get = vi.spyOn(services.api, "getTask").mockResolvedValue({
    ...base,
    liveSessions: [{ sessionID: "session-a", displayName: "First" }],
  });
  const article = await screen.findByRole("article", { name: card.title });
  fireEvent.click(article);
  expect(openDetail).toHaveBeenCalledWith(card.id);
  expect(get).not.toHaveBeenCalled();
  fireEvent.click(article, { shiftKey: true });
  await waitFor(() => {
    expect(openSession).toHaveBeenCalledWith("session-a");
  });
  expect(openDetail).toHaveBeenCalledTimes(1);
});

it.each(["running", "interrupted"] as const)(
  "opens the shared Session picker from the %s card context menu",
  async (statusKind) => {
    const { services, openSession, openDetail } = mount({ ...card, statusKind });
    const base = await services.api.getTask("task-1");
    vi.spyOn(services.api, "getTask").mockResolvedValue({
      ...base,
      liveSessions: [
        { sessionID: "session-a", displayName: "First" },
        { sessionID: "session-b", displayName: "Second" },
      ],
    });
    fireEvent.contextMenu(await screen.findByRole("article", { name: card.title }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Open in Chat" }));
    expect(openSession).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Second" }));
    await waitFor(() => {
      expect(openSession).toHaveBeenCalledWith("session-b");
    });
    expect(openDetail).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  },
);

it.each(["backlog", "done"] as const)("omits the Chat context action for a %s card", async (statusKind) => {
  mount({ ...card, statusKind });
  fireEvent.contextMenu(await screen.findByRole("article", { name: card.title }));
  await screen.findByRole("menu");
  expect(screen.queryByRole("menuitem", { name: "Open in Chat" })).not.toBeInTheDocument();
});
