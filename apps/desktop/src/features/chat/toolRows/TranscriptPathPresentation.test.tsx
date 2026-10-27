import { render, screen } from "@testing-library/react";
import { MotionConfig } from "motion/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, expect, it } from "bun:test";

import { appI18n, initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { TranscriptToolSlot } from "./TranscriptToolSlot";
import type { TranscriptToolSlotItem } from "./toolSlotTypes";

beforeAll(initializeI18n);

it("formats the image label without changing its source or offering Copy", async () => {
  const user = userEvent.setup({ delay: null });
  const path = "/Users/engineer/project/image.png";
  const item = {
    kind: "call",
    activity: "active",
    tool: {
      StepID: "step",
      ToolCallID: "image-call",
      ToolName: "view_image",
      Presentation: {
        ToolName: "view_image",
        Presentation: "default",
        RenderBehavior: "plain",
        IsShell: false,
        UserInitiated: false,
        Command: "",
        CompactText: "",
        InlineMeta: "",
        TimeoutLabel: "",
        PatchPresentation: null,
        RenderHint: { Kind: "plain", Path: path, ResultOnly: false, ShellDialect: "" },
        Question: "",
        Suggestions: [],
        RecommendedOptionIndex: 0,
        OmitSuccessfulResult: false,
        RawOutputRequested: false,
        OutputTruncated: false,
        MovedToBackground: false,
      },
    },
  } satisfies TranscriptToolSlotItem;
  const view = render(
    <MotionConfig skipAnimations>
      <TestAppProviders
        services={createTestServices([], undefined, { homePath: "/Users/engineer", platform: "macos" })}
      >
        <TranscriptToolSlot
          item={item}
          target={{ projectID: "project", sessionID: "session" }}
          effectiveWorkdir={null}
        />
      </TestAppProviders>
    </MotionConfig>,
  );
  expect(
    screen.getByText(appI18n.t("chat.toolRows.viewedImage", { path: "~/project/image.png" })),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.toolRows.expand") }));
  expect(screen.queryByRole("button", { name: appI18n.t("chat.toolRows.copy") })).not.toBeInTheDocument();
  expect(item.tool.Presentation.RenderHint.Path).toBe(path);
  view.unmount();
});
