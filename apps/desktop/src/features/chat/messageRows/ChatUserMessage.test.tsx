import { act, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "bun:test";

import { parsePendingWorkItemID } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { row } from "@/test-support/transcript-window";
import { SharedTextMotion } from "@/ui";
import { ChatMessageMotion } from "../ChatMessageMotion";
import type { OutgoingInput } from "../OutgoingInputPresentation";
import { ChatUserMessage } from "./ChatUserMessage";

it.each([100, 400])("preserves the original text flight when commitment arrives at %ims", async (delay) => {
  let now = Math.ceil(performance.now());
  const clock = vi.spyOn(performance, "now").mockImplementation(() => now);
  const bounds = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(20, 20, 240, 30));
  const motion = new SharedTextMotion();
  const services = createTestServices([]);
  const input: OutgoingInput = {
    inputID: parsePendingWorkItemID(crypto.randomUUID()),
    text: "A submitted message",
    intent: "send",
    placement: "bubble",
    confirmation: "awaiting",
    disposition: null,
  };
  const value = { Text: input.text, QueueItemIDs: [input.inputID.toJSONValue()] };
  const composer = document.createElement("textarea");
  composer.value = input.text;
  document.body.append(composer);
  motion.capture(input.inputID.toJSONValue(), composer);
  composer.remove();
  const content = (committed: boolean) => (
    <TestAppProviders services={services}>
      <ChatMessageMotion motion={motion}>
        {committed ? (
          <ChatUserMessage
            item={{
              kind: "user",
              state: "committed",
              key: "1:1",
              row: { ...row(1), User: value },
              value,
            }}
            edit={{ onEdit: vi.fn() }}
          />
        ) : (
          <ChatUserMessage preview={input} />
        )}
      </ChatMessageMotion>
    </TestAppProviders>
  );
  const view = render(content(false));
  const frame = async () => {
    await act(async () => {
      await new Promise(requestAnimationFrame);
      await new Promise(requestAnimationFrame);
    });
  };
  try {
    const text = within(view.container).getByText(input.text);
    const flyingText = screen.getAllByText(input.text).find((element) => element !== text);
    expect(flyingText).toBeInTheDocument();
    expect(text).not.toBeVisible();
    await frame();
    now += delay;
    await frame();
    view.rerender(content(true));

    expect(within(view.container).getByText(input.text)).toBe(text);
    if (delay < 350) {
      expect(flyingText).toBeInTheDocument();
      expect(text).not.toBeVisible();
      now += 349 - delay;
      await frame();
      expect(text).not.toBeVisible();
      now += 1;
      await frame();
    }
    expect(text).toBeVisible();
    expect(flyingText).not.toBeInTheDocument();
  } finally {
    view.unmount();
    motion.dispose();
    bounds.mockRestore();
    clock.mockRestore();
  }
});
