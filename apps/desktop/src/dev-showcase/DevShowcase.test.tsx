import { render, screen } from "@testing-library/react";
import { afterEach } from "vitest";

import { DevShowcaseApp } from "./DevShowcase";

describe("DevShowcaseApp", () => {
  afterEach(() => {
    document.documentElement.removeAttribute("data-builder-theme");
  });

  it("renders single-page UI inventory with mock data", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByTestId("dev-showcase-scroll-root")).toBeInTheDocument();
    expect(screen.getAllByTestId(/^showcase-section-/u).length).toBeGreaterThan(3);
  });

  it("does not render the removed handrolled toast stack in the showcase", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByTestId("dev-showcase-scroll-root")).toBeInTheDocument();
    expect(screen.queryAllByTestId("dev-showcase-toast-example")).toHaveLength(0);
  });
});
