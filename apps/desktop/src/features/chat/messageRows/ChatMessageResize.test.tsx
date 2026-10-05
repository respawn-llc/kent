import { act, render, screen, waitFor } from "@testing-library/react";

import type { TranscriptRenderItem } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { installAnimatedHeightGeometry } from "@/test-support/resize-observer";
import { row } from "@/test-support/transcript-window";

import { ChatAssistantMessage } from "./ChatAssistantMessage";
import { ChatUserMessage } from "./ChatUserMessage";

it("grows a streaming bubble smoothly without replacing already rendered characters", async () => {
  const geometry = installAnimatedHeightGeometry(80);
  const services = createTestServices([]);
  const content = (text: string) => (
    <TestAppProviders services={services}>
      <ChatAssistantMessage item={liveAssistant(text)} />
    </TestAppProviders>
  );
  const view = render(content("First line"));
  try {
    const bubble = screen.getByText(() => true, { selector: "section" });
    const firstCharacter = screen.getByText("F", { exact: true });
    expect(bubble.getBoundingClientRect().height).toBe(80);
    await flushAnimationFrames();

    view.rerender(content("First line\nSecond line"));
    act(() => {
      geometry.resize(160);
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
      geometry.resize(240);
    });
    expect(bubble.getBoundingClientRect().height).toBeCloseTo(intermediateHeight);
    await waitFor(() => {
      expect(bubble.getBoundingClientRect().height).toBeCloseTo(240);
    });
    expect(screen.getByText("F", { exact: true })).toBe(firstCharacter);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it("applies bubble height changes immediately when reduced motion is requested", async () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: query === "(prefers-reduced-motion: reduce)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
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
    vi.unstubAllGlobals();
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
  "animates a completed %s bubble shrinking after reflow",
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
          <ChatUserMessage
            item={{ kind, state: "committed", key: "1:1", row: committed, value: { Text: text } }}
            edit={{ onEdit: vi.fn() }}
          />
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
        geometry.resize(80);
      });
      expect(bubble.getBoundingClientRect().height).toBeGreaterThan(80);
      const intermediateHeight = await waitFor(() => {
        const height = bubble.getBoundingClientRect().height;
        expect(height).toBeLessThan(160);
        return height;
      });
      expect(intermediateHeight).toBeGreaterThan(80);
      await waitFor(() => {
        expect(bubble.getBoundingClientRect().height).toBeCloseTo(80);
      });
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
