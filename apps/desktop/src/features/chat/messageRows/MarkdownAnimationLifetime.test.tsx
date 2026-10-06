import { render, screen } from "@testing-library/react";

import { ChatAssistantMessage } from "./ChatAssistantMessage";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";

it.each([false, true])(
  "preserves pending and active same-line reveals across streaming updates (StrictMode: %s)",
  (strictMode) => {
    let now = 1000;
    const clock = vi.spyOn(performance, "now").mockImplementation(() => now);
    const services = createTestServices([]);
    const content = (text: string) => (
      <TestAppProviders services={services}>
        <ChatAssistantMessage
          item={{
            kind: "assistant",
            state: "live",
            key: "stream",
            value: { StepID: "step", StreamID: "stream", Phase: "commentary", Text: text },
          }}
        />
      </TestAppProviders>
    );
    const view = render(content("First"), { reactStrictMode: strictMode });
    try {
      now += 1;
      view.rerender(content("Firstx"));
      const character = screen.getByText("x", { exact: true });
      const duration = Number.parseFloat(character.style.getPropertyValue("--sd-duration"));
      const delay = Number.parseFloat(character.style.getPropertyValue("--sd-delay"));
      expect(duration).toBeGreaterThan(0);
      expect(delay).toBeGreaterThan(0);

      now += 1;
      view.rerender(content("Firstxy"));
      expect(Number.parseFloat(character.style.getPropertyValue("--sd-duration"))).toBe(duration);
      expect(Number.parseFloat(character.style.getPropertyValue("--sd-delay"))).toBe(delay);

      now += delay + duration / 2;
      view.rerender(content("Firstxyz"));
      expect(screen.getByText("x", { exact: true })).toBe(character);
      expect(Number.parseFloat(character.style.getPropertyValue("--sd-duration"))).toBe(duration);

      now += duration;
      view.rerender(content("Firstxyza"));
      expect(Number.parseFloat(character.style.getPropertyValue("--sd-duration"))).toBe(0);
    } finally {
      view.unmount();
      clock.mockRestore();
    }
  },
);
