import { expect, it, vi } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";

import type { TranscriptRenderItem } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { installAnimatedHeightGeometry } from "@/test-support/resize-observer";
import { row } from "@/test-support/transcript-window";

import { ChatAssistantMessage } from "./ChatAssistantMessage";
import { ChatUserMessage } from "./ChatUserMessage";
import { ChatMessageMotion } from "../ChatMessageMotion";
import { SharedTextMotion } from "@/ui";

function installElementOffsetWidth(element: HTMLElement, initialWidth: number) {
  const originalDescriptor = Object.getOwnPropertyDescriptor(element, "offsetWidth");
  let width = initialWidth;
  Object.defineProperty(element, "offsetWidth", {
    configurable: true,
    get: () => width,
  });
  return {
    set: (nextWidth: number) => {
      width = nextWidth;
    },
    restore: () => {
      if (originalDescriptor === undefined) Reflect.deleteProperty(element, "offsetWidth");
      else Object.defineProperty(element, "offsetWidth", originalDescriptor);
    },
  };
}

it("keeps the full initial layout height during ancestor entrance scaling", async () => {
  const geometry = installAnimatedHeightGeometry(160, 0.95);
  const view = render(
    <TestAppProviders services={createTestServices([])}>
      <ChatAssistantMessage item={liveAssistant("A response")} />
    </TestAppProviders>,
  );
  try {
    await flushAnimationFrames();
    const bubble = screen.getByText(() => true, { selector: "section" });
    expect(bubble.offsetHeight).toBe(160);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it("animates streaming growth in both dimensions without replacing rendered characters", async () => {
  const geometry = installAnimatedHeightGeometry(80);
  const services = createTestServices([]);
  const content = (text: string) => (
    <TestAppProviders services={services}>
      <ChatAssistantMessage item={liveAssistant(text)} />
    </TestAppProviders>
  );
  const view = render(content("First line"));
  let restoreOffsetWidth: (() => void) | undefined;
  try {
    const bubble = screen.getByText(() => true, { selector: "section" });
    const firstCharacter = screen.getByText("F", { exact: true });
    const chatRow = screen.getByText(() => true, { selector: ".chat-message-row" });
    const offsetWidth = installElementOffsetWidth(chatRow, 320);
    restoreOffsetWidth = offsetWidth.restore;
    expect(bubble.getBoundingClientRect().height).toBe(80);
    await flushAnimationFrames();

    view.rerender(content("First line\nSecond line"));
    act(() => {
      geometry.resize(160, 480);
    });

    expect(bubble.getBoundingClientRect().height).toBeLessThan(160);
    const intermediateHeight = await waitFor(() => {
      const height = bubble.getBoundingClientRect().height;
      expect(height).toBeGreaterThan(80);
      return height;
    });
    expect(intermediateHeight).toBeLessThan(160);
    view.rerender(content("First line\nSecond line\nThird line"));
    act(() => {
      geometry.resize(240, 560);
    });
    const continuingHeight = bubble.getBoundingClientRect().height;
    expect(continuingHeight).toBeGreaterThan(80);
    expect(continuingHeight).toBeLessThan(160);
    await waitFor(() => {
      expect(bubble.getBoundingClientRect().height).toBeCloseTo(240);
    });
    expect(screen.getByText("F", { exact: true })).toBe(firstCharacter);
  } finally {
    view.unmount();
    restoreOffsetWidth?.();
    geometry.restore();
  }
});

it("interrupts streaming height motion for Chat reflow and animates subsequent content growth", async () => {
  const geometry = installAnimatedHeightGeometry(80);
  const view = render(
    <TestAppProviders services={createTestServices([])}>
      <ChatAssistantMessage item={liveAssistant("A streaming response")} />
    </TestAppProviders>,
  );
  let restoreOffsetWidth: (() => void) | undefined;
  try {
    const bubble = screen.getByText(() => true, { selector: "section" });
    const chatRow = screen.getByText(() => true, { selector: ".chat-message-row" });
    const offsetWidth = installElementOffsetWidth(chatRow, 320);
    restoreOffsetWidth = offsetWidth.restore;
    await flushAnimationFrames();
    act(() => {
      geometry.resize(240, 480);
    });
    await waitFor(() => {
      expect(bubble.getBoundingClientRect().height).toBeGreaterThan(80);
    });
    act(() => {
      offsetWidth.set(240);
      geometry.resize(160, 240);
    });
    await flushAnimationFrames();
    expect(bubble.getBoundingClientRect().height).toBe(160);
    act(() => {
      geometry.resize(240, 280);
    });
    const intermediateHeight = await waitFor(() => {
      const height = bubble.getBoundingClientRect().height;
      expect(height).toBeGreaterThan(160);
      return height;
    });
    expect(intermediateHeight).toBeLessThan(240);
    await waitFor(() => {
      expect(bubble.getBoundingClientRect().height).toBeCloseTo(240);
    });
  } finally {
    view.unmount();
    restoreOffsetWidth?.();
    geometry.restore();
  }
});

it("applies bubble height changes immediately when reduced motion is requested", async () => {
  const matchMedia = Object.getOwnPropertyDescriptor(globalThis, "matchMedia");
  Object.defineProperty(globalThis, "matchMedia", {
    configurable: true,
    value: vi.fn((query: string) => ({
      matches: query === "(prefers-reduced-motion: reduce)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
  const geometry = installAnimatedHeightGeometry(80);
  const view = render(
    <TestAppProviders services={createTestServices([])}>
      <ChatAssistantMessage item={liveAssistant("A streaming response")} />
    </TestAppProviders>,
  );
  try {
    const bubble = screen.getByText(() => true, { selector: "section" });
    await flushAnimationFrames();
    act(() => {
      geometry.resize(160);
    });
    await flushAnimationFrames();
    expect(bubble.getBoundingClientRect().height).toBe(160);
    act(() => {
      geometry.resize(80);
    });
    await flushAnimationFrames();
    expect(bubble.getBoundingClientRect().height).toBe(80);
  } finally {
    view.unmount();
    geometry.restore();
    if (matchMedia) Object.defineProperty(globalThis, "matchMedia", matchMedia);
    else Reflect.deleteProperty(globalThis, "matchMedia");
  }
});

function liveAssistant(text: string): Extract<TranscriptRenderItem, { kind: "assistant"; state: "live" }> {
  return {
    kind: "assistant",
    state: "live",
    key: "stream",
    value: { StepID: "step", StreamID: "stream", Phase: "commentary", Text: text },
  };
}

it.each(["user", "assistant"] as const)(
  "keeps a completed %s bubble fitted to its content during width reflow",
  async (kind) => {
    const geometry = installAnimatedHeightGeometry(160);
    const services = createTestServices([]);
    const committed = row(1);
    const text = "A completed message that wraps onto multiple lines.";
    const assistant = {
      StepID: "step",
      StreamID: "stream",
      Phase: "commentary" as const,
      Text: text,
      CondensedText: null,
      committed_at_unix_ms: null,
    };
    const view = render(
      <TestAppProviders services={services}>
        {kind === "user" ? (
          <ChatMessageMotion motion={new SharedTextMotion()}>
            <ChatUserMessage
              item={{
                kind,
                state: "committed",
                key: "1:1",
                row: committed,
                value: { Text: text, QueueItemIDs: [] },
              }}
              edit={{ onEdit: vi.fn() }}
            />
          </ChatMessageMotion>
        ) : (
          <ChatAssistantMessage
            item={{
              kind,
              state: "committed",
              key: "1:1",
              row: { ...committed, Kind: kind, User: null, Assistant: assistant },
              value: assistant,
            }}
          />
        )}
      </TestAppProviders>,
    );
    try {
      const bubble = screen.getByText(() => true, { selector: "section" });
      await flushAnimationFrames();
      act(() => {
        geometry.resize(80, 480);
      });
      await flushAnimationFrames();
      expect(bubble.getBoundingClientRect().height).toBe(80);
      act(() => {
        geometry.resize(200, 240);
      });
      await flushAnimationFrames();
      expect(bubble.getBoundingClientRect().height).toBe(200);
    } finally {
      view.unmount();
      geometry.restore();
    }
  },
);

async function flushAnimationFrames() {
  await act(async () => {
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
}
