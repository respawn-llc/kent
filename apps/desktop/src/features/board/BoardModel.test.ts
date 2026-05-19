import type { BoardColumn, WorkflowBoard } from "../../api";
import { boardSections } from "./BoardModel";

describe("boardSections", () => {
  it("orders Backlog first, grouped nodes next, and Done last", () => {
    expect(boardSections(board).map((section) => section.id)).toEqual(["backlog", "group-1", "done"]);
  });
});

const backlogColumn: BoardColumn = {
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

const activeColumn: BoardColumn = {
  assigneeRole: "coder",
  groupID: "group-1",
  id: "node-1",
  isBacklog: false,
  isDone: false,
  key: "implement",
  name: "Implement",
  sortOrder: 1,
  taskCount: 1,
};

const doneColumn: BoardColumn = {
  assigneeRole: "",
  groupID: "",
  id: "done",
  isBacklog: false,
  isDone: true,
  key: "done",
  name: "Done",
  sortOrder: 2,
  taskCount: 0,
};

const board: WorkflowBoard = {
  columns: [doneColumn, activeColumn, backlogColumn],
  generatedAt: 1,
  groups: [{ id: "group-1", key: "core", name: "Core", nodeIDs: ["node-1"], sortOrder: 1 }],
  latestEventSequence: 1,
  projectID: "project-1",
  projectKey: "PROJ",
  projectName: "Project",
  selectedWorkflow: {
    description: "",
    graphRevision: 1,
    id: "workflow-1",
    isProjectDefault: true,
    name: "Workflow",
    validForTaskCreation: true,
    validationErrors: [],
  },
  workflows: [],
};
