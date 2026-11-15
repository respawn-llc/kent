import { expect, it } from "bun:test";
import { render, screen } from "@testing-library/react";
import { ReducedMotionContext } from "@/ui";
import { OverlappingCrossfade } from "./OverlappingCrossfade";

function page(key: string, reduced: boolean) {
  return (
    <ReducedMotionContext value={reduced}>
      <OverlappingCrossfade contentKey={key}>
        <div>{key}</div>
      </OverlappingCrossfade>
    </ReducedMotionContext>
  );
}

it("replaces the previous destination immediately when motion is reduced", () => {
  const view = render(page("projects", true));
  view.rerender(page("workflows", true));
  expect(screen.queryByText("projects")).not.toBeInTheDocument();
  expect(screen.getByText("workflows")).toBeInTheDocument();
});

it("removes an outgoing destination when reduced motion is enabled during a transition", () => {
  const view = render(page("projects", false));
  view.rerender(page("workflows", false));
  expect(screen.getByText("projects")).toBeInTheDocument();
  view.rerender(page("workflows", true));
  expect(screen.queryByText("projects")).not.toBeInTheDocument();
  expect(screen.getByText("workflows")).toBeInTheDocument();
});
