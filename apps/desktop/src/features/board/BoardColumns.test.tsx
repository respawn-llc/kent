import { render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { beforeAll } from "vitest";

import type { BoardCard, BoardColumn } from "../../api";
import { appI18n, initializeI18n } from "../../i18n/setup";
import { KanbanColumn } from "./BoardColumns";

describe("KanbanColumn", () => {
  beforeAll(async () => {
    await initializeI18n();
  });

  it("renders load-more with shared spinner and hidden accessible label", () => {
    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          canRunTasks
          canToggleDone={false}
          cards={[card]}
          column={column}
          doneExpanded={false}
          hasMoreCards
          isFirstActive={false}
          isLoadingMoreCards
          onCardClick={() => undefined}
          onDropTask={() => undefined}
          onInterruptTask={() => undefined}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
          onToggleDone={() => undefined}
        />
      </I18nextProvider>,
    );

    expect(screen.getByRole("status", { name: "Loading more" })).toContainElement(screen.getByTestId("spinner"));
    expect(screen.getByText("Loading more")).toHaveClass("sr-only");
  });
});

const column: BoardColumn = {
  assigneeRole: "",
  groupID: "",
  id: "backlog",
  isBacklog: true,
  isDone: false,
  key: "backlog",
  name: "Backlog",
  sortOrder: 0,
  taskCount: 1,
};

const card: BoardCard = {
  actions: {
    canCancel: false,
    canInterrupt: false,
    canResume: false,
    canStart: true,
    interruptRunID: "",
    manualMoveTargetNodeIDs: [],
    needsDetailForInterrupt: false,
    needsDetailForResume: false,
    resumeRunID: "",
  },
  activeNodeIDs: [],
  bodyPreview: "Body",
  id: "task-1",
  shortID: "T-1",
  sourceWorkspace: {
    availability: "available",
    id: "workspace-1",
    isPrimary: true,
    name: "Main",
    rootPath: "/tmp/project",
    updatedAt: 1,
  },
  status: {
    attentionTypes: [],
    kind: "backlog",
    label: "Backlog",
    nativeState: "backlog",
    nodeIDs: [],
    runIDs: [],
  },
  title: "Task",
  updatedAt: 1,
  workflowID: "workflow-1",
};
