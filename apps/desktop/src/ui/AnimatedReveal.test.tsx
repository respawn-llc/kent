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
