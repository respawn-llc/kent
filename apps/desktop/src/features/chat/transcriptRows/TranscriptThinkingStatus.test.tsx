import { beforeAll, expect, it } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { MotionConfig } from "motion/react";
import { initializeI18n } from "@/i18n";
import { ReducedMotionContext } from "@/ui";
import { TranscriptThinkingStatus } from "./TranscriptThinkingStatus";

beforeAll(initializeI18n);

function status(text: string, reduced = false) {
  return (
    <ReducedMotionContext value={reduced}>
      <TranscriptThinkingStatus presentation={{ kind: "text", text }} />
    </ReducedMotionContext>
  );
}

it("retires replaced status text after the transition completes", async () => {
  const view = render(<MotionConfig skipAnimations>{status("first activity")}</MotionConfig>);
  view.rerender(<MotionConfig skipAnimations>{status("second activity")}</MotionConfig>);
  expect(screen.getByText("second activity")).toBeInTheDocument();
  await waitFor(() => expect(screen.queryByText("first activity")).not.toBeInTheDocument());
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
