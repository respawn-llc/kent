import { afterEach, beforeAll, beforeEach, expect, it, vi } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { initializeI18n } from "@/i18n";
import { ReducedMotionContext } from "@/ui";
import { TranscriptThinkingStatus } from "./TranscriptThinkingStatus";

beforeAll(initializeI18n);

let now: number;
let restoreClock: () => void;
beforeEach(() => {
  now = Math.ceil(performance.now());
  const clock = vi.spyOn(performance, "now").mockImplementation(() => now);
  restoreClock = () => {
    clock.mockRestore();
  };
});
afterEach(() => {
  restoreClock();
});

function status(text: string, reduced = false) {
  return (
    <ReducedMotionContext value={reduced}>
      <TranscriptThinkingStatus presentation={{ kind: "text", text }} />
    </ReducedMotionContext>
  );
}

it("retains outgoing status text during replacement and then retires it", async () => {
  const view = render(status("first activity"));
  view.rerender(status("second activity"));
  expect(screen.getByText("first activity")).toBeInTheDocument();
  expect(screen.getByText("second activity")).toBeInTheDocument();
  await waitFor(() => {
    now += 1_000;
    expect(screen.queryByText("first activity")).not.toBeInTheDocument();
  });
  expect(screen.getByText("second activity")).toBeVisible();
});

it("replaces status text immediately with reduced motion", () => {
  const view = render(status("first activity", true));
  view.rerender(status("second activity", true));
  expect(screen.queryByText("first activity")).not.toBeInTheDocument();
  expect(screen.getByText("second activity")).toBeVisible();
});

it("retires outgoing status text when reduced motion is enabled mid-transition", () => {
  const view = render(status("first activity"));
  view.rerender(status("second activity"));
  expect(screen.getByText("first activity")).toBeInTheDocument();
  view.rerender(status("second activity", true));
  expect(screen.queryByText("first activity")).not.toBeInTheDocument();
  expect(screen.getByText("second activity")).toBeVisible();
});
