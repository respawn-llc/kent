import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, expect, it } from "vitest";

import { appI18n, initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { TranscriptToolSlot } from "./TranscriptToolSlot";
import type { TranscriptToolSlotItem } from "./toolSlotTypes";

beforeAll(initializeI18n);

it("collapses the structured image label but copies the original absolute path", async () => {
  const user = userEvent.setup();
  const path = "/Users/engineer/project/image.png";
  const item = {
    kind: "live",
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
  render(
    <TestAppProviders
      services={createTestServices([], undefined, { homePath: "/Users/engineer", platform: "macos" })}
    >
      <TranscriptToolSlot item={item} />
    </TestAppProviders>,
  );
  expect(
    screen.getByText(appI18n.t("chat.toolRows.viewedImage", { path: "~/project/image.png" })),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.toolRows.expand") }));
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.toolRows.copy") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe(appI18n.t("chat.toolRows.viewedImage", { path }));
  });
  expect(item.tool.Presentation.RenderHint.Path).toBe(path);
});
