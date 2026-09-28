import { screen, waitFor } from "@testing-library/react";

import { RpcError, rpcErrorCodes } from "@/api";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { mountTaskDetailSurface, taskDetailResponse } from "@/test-support/task-detail";
import { appI18n } from "@/i18n";

describe("TaskDetailSurface sidebar ownership", () => {
  it("dismisses a typed missing Task before mounting content or capture ownership", async () => {
    const page = createTestSidebarNavigator();
    mountTaskDetailSurface(taskDetailResponse, {
      navigator: page,
      routes: [
        {
          method: "workflow.task.get",
          handler: () => {
            throw new RpcError({
              code: rpcErrorCodes.workflowTaskNotFound,
              message: "gone",
              method: "workflow.task.get",
            });
          },
        },
      ],
    });
    await waitFor(() => {
      expect(page.back).toHaveBeenCalledOnce();
    });
    expect(page.registerCapture).not.toHaveBeenCalled();
  });

  it("keeps ordinary failures in the existing recovery surface", async () => {
    const page = createTestSidebarNavigator();
    mountTaskDetailSurface(taskDetailResponse, {
      navigator: page,
      routes: [
        {
          method: "workflow.task.get",
          handler: () => {
            throw new Error("offline");
          },
        },
      ],
    });
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(page.back).not.toHaveBeenCalled();
  });

  it("opens without sidebar capture ownership", async () => {
    mountTaskDetailSurface(taskDetailResponse);
    expect(await screen.findByRole("textbox", { name: appI18n.t("task.name") })).toBeInTheDocument();
  });
});
