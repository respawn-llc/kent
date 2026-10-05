import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  taskResumeRoute,
  taskResumeNeedsTarget,
  taskResumeApplied,
  mountTaskDetailSurface,
  taskDetailResponseWithInterruptedCurrentScript,
} from "@/test-support/task-detail";

it("reuses one Task Detail Resume continuation for target selection", async () => {
  let resumeCalls = 0;
  const services = mountTaskDetailSurface(taskDetailResponseWithInterruptedCurrentScript, {
    routes: [
      taskResumeRoute(() => {
        resumeCalls += 1;
        return resumeCalls === 1 ? taskResumeNeedsTarget() : taskResumeApplied(["node-script"]);
      }),
    ],
  });
  const resume = vi.spyOn(services.api, "resumeTask");
  const user = userEvent.setup();
  const resumeButtons = await screen.findAllByTestId("task-detail-resume");
  const resumeButton = resumeButtons[0];
  if (resumeButton === undefined) {
    throw new Error("Expected a Task Detail Resume button.");
  }

  await user.click(resumeButton);
  await screen.findByTestId("execution-target-submit");
  const firstRequest = resume.mock.calls[0]?.[0];
  if (firstRequest === undefined) throw new Error("Resume was not requested.");

  await user.click(screen.getByTestId("execution-target-submit"));
  await waitFor(() => {
    expect(resume).toHaveBeenCalledTimes(2);
  });
  expect(resume.mock.calls[1]?.[0]).toMatchObject({
    setupOperationID: firstRequest.setupOperationID,
    executionTarget: { mode: "default_branch" },
  });
});
