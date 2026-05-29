import { shouldSkipNativeDialogStartupGate } from "./routes";
import { routeFramePaddingClassName, routeUsesEdgeToEdgeLayout } from "./routeLayout";

describe("native dialog startup gate policy", () => {
  it("only skips startup readiness for event-only native dialogs", () => {
    expect(shouldSkipNativeDialogStartupGate("/native-dialog/workspace-unlink")).toBe(true);
    expect(shouldSkipNativeDialogStartupGate("/native-dialog/workflow-delete-confirm")).toBe(true);
    expect(shouldSkipNativeDialogStartupGate("/native-dialog/project-create")).toBe(false);
    expect(shouldSkipNativeDialogStartupGate("/native-dialog/new-task")).toBe(false);
    expect(shouldSkipNativeDialogStartupGate("/native-dialog/task-detail")).toBe(false);
  });
});

describe("route layout policy", () => {
  it("renders workflow editor as an edge-to-edge canvas route", () => {
    expect(routeUsesEdgeToEdgeLayout("/workflows/workflow-1/editor")).toBe(true);
  });

  it("uses the default four-sided route gutter for workflow library", () => {
    expect(routeUsesEdgeToEdgeLayout("/workflows")).toBe(false);
    expect(routeFramePaddingClassName("/workflows")).toBe("p-[var(--space-2)]");
  });
});
