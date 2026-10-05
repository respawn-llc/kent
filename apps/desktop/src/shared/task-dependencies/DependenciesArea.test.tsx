import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import type { ComponentProps, ReactElement } from "react";

import type { TaskDependencies } from "@/api";
import type { TaskSearchResult } from "@/app-facade";
import { DependenciesArea } from "./DependenciesArea";
import { dependencyRelatedTaskIDs } from "./dependencyCache";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";

let services = createTestServices([]);
function TaskDependenciesArea(
  props: Omit<ComponentProps<typeof DependenciesArea>, "interaction" | "excludedTaskIDs">,
) {
  return (
    <DependenciesArea
      {...props}
      interaction={{ kind: "persisted", taskID: "task-1" }}
      excludedTaskIDs={() => new Set(["task-1", ...dependencyRelatedTaskIDs(props.dependencies)])}
    />
  );
}
function renderArea(element: ReactElement) {
  return render(<TestAppProviders services={services}>{element}</TestAppProviders>);
}

const searchFixture = vi.hoisted<{ results: readonly TaskSearchResult[] }>(() => ({ results: [] }));
vi.mock("react-i18next", async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
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
    results: searchFixture.results,
    searchable: searchFixture.results.length > 0,
  }),
}));

const dependencies: TaskDependencies = {
  blockerCount: 1,
  unsatisfiedBlockerCount: 1,
  directlyBlockedTaskCount: 1,
  directions: [
    {
      direction: "blocked-by",
      totalCount: 1,
      unsatisfiedCount: 1,
      addAvailability: { kind: "available", remainingCapacity: 4 },
      items: [
        {
          taskID: "task-2",
          shortID: "KENT-2",
          title: "Prepare release",
          workflowID: "workflow-2",
          satisfaction: "unsatisfied",
          status: {
            kind: "backlog",
            nativeState: "active",
            nodeIDs: [],
            attentionTypes: [],
          },
        },
      ],
    },
    {
      direction: "blocks",
      totalCount: 1,
      unsatisfiedCount: null,
      addAvailability: { kind: "available", remainingCapacity: 3 },
      items: [
        {
          taskID: "task-3",
          shortID: "KENT-3",
          title: "Publish release",
          workflowID: "workflow-3",
          satisfaction: null,
          status: {
            kind: "running",
            nativeState: "running",
            nodeIDs: ["node-1"],
            attentionTypes: [],
          },
        },
      ],
    },
  ],
};

describe("TaskDependenciesArea", () => {
  beforeEach(() => {
    searchFixture.results = [];
    services = createTestServices([]);
    vi.spyOn(services.api, "addTaskDependency").mockImplementation(async (blockerTaskID, blockedTaskID) => ({
      outcome: "added",
      blockerTaskID,
      blockedTaskID,
      blockerShortID: "KNT-2",
      blockedShortID: "KNT-1",
    }));
    vi.spyOn(services.api, "removeTaskDependency").mockImplementation(
      async (blockerTaskID, blockedTaskID) => ({
        outcome: "removed",
        blockerTaskID,
        blockedTaskID,
        blockerShortID: "KNT-2",
        blockedShortID: "KNT-1",
      }),
    );
  });

  it("keeps both empty directions actionable without progress", () => {
    renderArea(
      <TaskDependenciesArea
        dependencies={{
          blockerCount: 0,
          unsatisfiedBlockerCount: 0,
          directlyBlockedTaskCount: 0,
          directions: dependencies.directions.map((direction) => ({
            ...direction,
            totalCount: 0,
            unsatisfiedCount: direction.direction === "blocked-by" ? 0 : null,
            items: [],
          })),
        }}
        navigationDisabled={false}
        onAdd={vi.fn()}

        onSelectTask={vi.fn()}
        projectID="project-1"
      />,
    );

    expect(screen.getAllByRole("group")).toHaveLength(2);
    expect(screen.getAllByTestId(/^dependency-add-/)).toHaveLength(2);
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("uses server-owned limit availability to disable Add", () => {
    renderArea(
      <TaskDependenciesArea
        dependencies={{
          ...dependencies,
          directions: dependencies.directions.map((direction) =>
            direction.direction === "blocked-by"
              ? {
                  ...direction,
                  addAvailability: { kind: "limit_reached" as const },
                }
              : direction,
          ),
        }}
        navigationDisabled={false}
        onAdd={vi.fn()}

        onSelectTask={vi.fn()}
        projectID="project-1"
      />,
    );

    const add = screen.getByTestId("dependency-add-blocked-by");
    expect(add).toBeDisabled();
    expect(add).toHaveAttribute("aria-describedby");
  });

  it("blocks navigation while retaining independent relationship removal", () => {
    renderArea(
      <TaskDependenciesArea
        dependencies={dependencies}
        navigationDisabled
        onAdd={vi.fn()}

        onSelectTask={vi.fn()}
        projectID="project-1"
      />,
    );

    expect(screen.getByTestId("dependency-add-blocked-by")).toBeDisabled();
    expect(screen.getByTestId("dependency-remove-task-2")).toBeEnabled();
  });

  it("renders both typed directions and delegates relationship actions", async () => {
    const onAdd = vi.fn();
    const onRemove = vi.spyOn(services.api, "removeTaskDependency");
    const onSelectTask = vi.fn();
    const user = userEvent.setup();

    renderArea(
      <TaskDependenciesArea
        dependencies={dependencies}
        navigationDisabled={false}
        onAdd={onAdd}

        onSelectTask={onSelectTask}
        projectID="project-1"
      />,
    );

    expect(screen.getAllByRole("group")).toHaveLength(2);
    expect(screen.getByText("KENT-2")).toBeInTheDocument();
    expect(screen.getByText("Prepare release")).toBeInTheDocument();
    const progress = screen.getByRole("progressbar");
    expect(progress).toHaveAttribute("aria-valuenow", "0");
    expect(progress).toHaveAttribute("aria-valuemax", "1");
    expect(
      screen.queryAllByRole("button").some((button) => within(button).queryByRole("progressbar") !== null),
    ).toBe(false);
    const chip = screen.getByLabelText(progress.getAttribute("aria-label") ?? "", {
      selector: "span[tabindex]",
    });
    await user.tab();
    expect(chip).toHaveFocus();
    expect(await screen.findByRole("tooltip")).not.toBeEmptyDOMElement();

    await user.click(screen.getByTestId("dependency-row-task-2"));
    await user.click(screen.getByTestId("dependency-add-blocked-by"));
    await user.click(screen.getByRole("button", { name: "task.dependenciesCreateTask" }));
    await user.click(screen.getByTestId("dependency-remove-task-2"));

    expect(onSelectTask).toHaveBeenCalledWith("task-2");
    expect(onAdd).toHaveBeenCalledWith("blocked-by");
    expect(onRemove).toHaveBeenCalledWith("task-2", "task-1");
  });

  it("adds an existing searched Task and resets accepted IDs after close", async () => {
    const onAddExisting = vi.spyOn(services.api, "addTaskDependency");
    const user = userEvent.setup();
    searchFixture.results = [
      {
        key: "candidate",
        group: {
          projectID: "project-1",
          projectKey: "KENT",
          taskID: "task-9",
          shortID: "KENT-9",
          workflowID: "workflow-1",
          title: "Existing candidate",
          status: {
            kind: "backlog",
            nativeState: "active",
            nodeIDs: [],
            attentionTypes: [],
          },
          totalHitCount: 1,
          hits: [
            {
              ordinal: 1,
              source: { kind: "title" },
              literal: {
                before: "",
                match: "Existing",
                after: " candidate",
                leftTruncated: false,
                rightTruncated: false,
              },
            },
          ],
        },
      },
    ];

    renderArea(
      <TaskDependenciesArea
        dependencies={{
          blockerCount: 0,
          unsatisfiedBlockerCount: 0,
          directlyBlockedTaskCount: 0,
          directions: dependencies.directions.map((direction) => ({
            ...direction,
            totalCount: 0,
            unsatisfiedCount: direction.direction === "blocked-by" ? 0 : null,
            items: [],
          })),
        }}
        navigationDisabled={false}
        onAdd={vi.fn()}

        onSelectTask={vi.fn()}
        projectID="project-1"
      />,
    );

    await user.click(screen.getByTestId("dependency-add-blocked-by"));
    await user.click(screen.getByTestId("dependency-candidate-task-9"));

    expect(onAddExisting).toHaveBeenCalledWith("task-9", "task-1");
    expect(screen.queryByTestId("dependency-candidate-task-9")).not.toBeInTheDocument();
    await user.click(screen.getByTestId("dependency-add-blocked-by"));
    await user.click(screen.getByTestId("dependency-add-blocked-by"));
    expect(screen.getByTestId("dependency-candidate-task-9")).toBeInTheDocument();
  });

  it("blocks relationship navigation while keeping Remove independently available", async () => {
    const onAdd = vi.fn();
    const onRemove = vi.spyOn(services.api, "removeTaskDependency");
    const onSelectTask = vi.fn();
    const user = userEvent.setup();

    renderArea(
      <TaskDependenciesArea
        dependencies={dependencies}
        navigationDisabled
        onAdd={onAdd}

        onSelectTask={onSelectTask}
        projectID="project-1"
      />,
    );

    expect(screen.getByTestId("dependency-add-blocked-by")).toBeDisabled();
    const dependencyButton = screen
      .getAllByRole("button")
      .find((button) => within(button).queryByTestId("dependency-row-task-2") !== null);
    if (dependencyButton === undefined) throw new Error("Expected the dependency row button.");
    expect(dependencyButton).toBeDisabled();
    expect(screen.getByTestId("dependency-remove-task-2")).toBeEnabled();

    await user.click(screen.getByTestId("dependency-remove-task-2"));
    expect(onRemove).toHaveBeenCalledOnce();
    expect(onAdd).not.toHaveBeenCalled();
    expect(onSelectTask).not.toHaveBeenCalled();
  });
});
