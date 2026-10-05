import { act, screen, waitFor } from "@testing-library/react";
import { appI18n } from "@/i18n";
import { projectEventsFixture } from "@/test-support/project-events";
import {
  emptyTaskAttentionResponse,
  mountTaskDetailSurface,
  taskDetailResponse,
  taskCommentPage,
  taskCommentRoute,
} from "@/test-support/task-detail";

describe("Task Detail feed presentation", () => {
  it("renders the default empty Comments state from the shared test service", async () => {
    mountTaskDetailSurface(taskDetailResponse, { attention: emptyTaskAttentionResponse });
    expect(await screen.findByText(appI18n.t("task.noCommentsTitle"))).toBeInTheDocument();
  });

  it("renders adjacent repeated Comment identities as separate feed rows", async () => {
    mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      comments: taskCommentPage([
        { id: "comment-duplicate", body: "First occurrence", milliseconds: 2 },
        { id: "comment-duplicate", body: "Second occurrence", milliseconds: 1 },
      ]),
    });
    await waitFor(() => {
      expect(screen.getByText("First occurrence")).toBeInTheDocument();
      expect(screen.getByText("Second occurrence")).toBeInTheDocument();
    });
    expect(screen.getAllByRole("article")).toHaveLength(2);
  });

  it("uses the authoritative Comment total for the tab badge", async () => {
    mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      comments: taskCommentPage(
        [{ id: "comment-window-item", body: "Only retained window item", milliseconds: 2 }],
        501,
      ),
    });
    const commentsTab = await screen.findByRole("tab", { name: new RegExp(appI18n.t("task.comments")) });
    expect(commentsTab).toHaveTextContent("501");
  });

  it("refetches the retained newest page on a live update without clearing visible rows", async () => {
    let latestCommentBody = "Initial comment";
    const requests: { taskID: string; offset: number }[] = [];
    const services = mountTaskDetailSurface(taskDetailResponse, {
      attention: emptyTaskAttentionResponse,
      routes: [
        taskCommentRoute((taskID, _callIndex, offset) => {
          requests.push({ taskID, offset });
          return taskCommentPage([{ id: "comment-live", body: latestCommentBody, milliseconds: 2 }]);
        }),
      ],
    });
    expect(await screen.findByText("Initial comment")).toBeInTheDocument();
    await waitFor(() => {
      expect(projectEventsFixture(services.transport).activeCount).toBeGreaterThan(0);
    });
    latestCommentBody = "Updated comment";
    act(() => {
      projectEventsFixture(services.transport).emit({ action: "updated" });
    });
    expect(screen.getByText("Initial comment")).toBeInTheDocument();
    await screen.findByText("Updated comment");
    expect(requests).toEqual([
      { taskID: "task-1", offset: 0 },
      { taskID: "task-1", offset: 0 },
    ]);
  });
});
