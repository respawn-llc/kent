import { expect, it } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { AnimatePresence } from "motion/react";
import { ReducedMotionContext } from "./motion";
import { AnimatedReveal } from "./AnimatedReveal";

it("keeps queued content visible when a reduced-motion sheet opens and changes", async () => {
  const sheet = (items: readonly string[]) => (
    <ReducedMotionContext value={true}>
      <AnimatePresence initial={false}>
        {items.length > 0 && (
          <AnimatedReveal key="sheet">
            <AnimatePresence initial={false}>
              {items.map((id) => (
                <AnimatedReveal key={id} variant="collapse-center">
                  <span>{id}</span>
                </AnimatedReveal>
              ))}
            </AnimatePresence>
          </AnimatedReveal>
        )}
      </AnimatePresence>
    </ReducedMotionContext>
  );
  const view = render(sheet([]));
  view.rerender(sheet(["steering"]));
  await waitFor(() => expect(screen.getByText("steering")).toBeVisible());
  view.rerender(sheet(["queued"]));
  await waitFor(() => {
    expect(screen.queryByText("steering")).not.toBeInTheDocument();
    expect(screen.getByText("queued")).toBeVisible();
  });
});

it("removes exiting content when reduced motion is enabled during its transition", async () => {
  const content = (visible: boolean, reducedMotion: boolean) => (
    <ReducedMotionContext value={reducedMotion}>
      <AnimatePresence initial={false}>
        {visible && (
          <AnimatedReveal key="outgoing">
            <span data-testid="outgoing-content" />
          </AnimatedReveal>
        )}
      </AnimatePresence>
      <span data-testid="retained-content" />
    </ReducedMotionContext>
  );
  const view = render(content(true, false));
  const retained = screen.getByTestId("retained-content");
  view.rerender(content(false, false));
  expect(screen.getByTestId("outgoing-content")).toBeInTheDocument();
  view.rerender(content(false, true));
  expect(screen.queryByTestId("outgoing-content")).not.toBeInTheDocument();
  expect(screen.getByTestId("retained-content")).toBe(retained);
});
