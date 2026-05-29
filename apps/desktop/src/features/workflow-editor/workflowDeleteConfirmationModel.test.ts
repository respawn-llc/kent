import {
  workflowDeleteConfirmationWindowOptions,
  workflowDeleteConfirmationWindowTargetFromSearch,
} from "./workflowDeleteConfirmationModel";

describe("workflowDeleteConfirmationModel", () => {
  it("round-trips native dialog target params", () => {
    const options = workflowDeleteConfirmationWindowOptions({
      counts: {
        edgeCount: 2,
        nodeCount: 1,
        transitionGroupCount: 3,
      },
      requestID: "workflow-1-delete-4",
      title: "Confirm graph delete",
    });

    expect(options).toMatchObject({
      initialHeight: 260,
      initialWidth: 420,
      label: "workflow-delete-workflow-1-delete-4",
      params: {
        edgeCount: "2",
        nodeCount: "1",
        requestID: "workflow-1-delete-4",
        transitionGroupCount: "3",
      },
      route: "/native-dialog/workflow-delete-confirm",
      title: "Confirm graph delete",
    });
    expect(workflowDeleteConfirmationWindowTargetFromSearch(options.params)).toEqual({
      counts: {
        edgeCount: 2,
        nodeCount: 1,
        transitionGroupCount: 3,
      },
      requestID: "workflow-1-delete-4",
    });
  });

  it("clamps missing and malformed native dialog counts", () => {
    expect(
      workflowDeleteConfirmationWindowTargetFromSearch({
        edgeCount: "-1",
        nodeCount: "",
        requestID: "delete-invalid",
        transitionGroupCount: "2.5",
      }),
    ).toEqual({
      counts: {
        edgeCount: 0,
        nodeCount: 0,
        transitionGroupCount: 0,
      },
      requestID: "delete-invalid",
    });
  });
});
