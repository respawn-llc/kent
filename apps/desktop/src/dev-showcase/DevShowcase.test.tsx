import { render, screen } from "@testing-library/react";
import { afterEach } from "vitest";

import { DevShowcaseApp } from "./DevShowcase";

describe("DevShowcaseApp", () => {
  afterEach(() => {
    document.documentElement.removeAttribute("data-builder-theme");
  });

  it("renders single-page UI inventory with mock data", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByRole("heading", { name: "Builder UI Showcase" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "UI Primitives" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Kanban Board" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Hover Menu States" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Task Detail" })).toBeInTheDocument();
    expect(await screen.findByText("Inventory desktop UI components")).toBeInTheDocument();
    expect(screen.getByText("Question")).toBeInTheDocument();
    expect(screen.getByText("Approval")).toBeInTheDocument();
  });

  it("keeps showcase scrollable and overlay examples in normal document flow", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByTestId("dev-showcase-scroll-root")).toHaveClass(
      "h-screen",
      "overflow-y-auto",
      "overflow-x-hidden",
    );
    for (const element of [
      screen.getByTestId("dev-showcase-toast-triggers"),
      screen.getByTestId("dev-showcase-floating-example"),
    ]) {
      expect(element.className).not.toContain("fixed");
      expect(element.className).not.toContain("absolute");
    }
  });

  it("does not render the removed handrolled toast stack in the showcase", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByRole("heading", { name: "Sonner Toasts" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Toast Stack" })).not.toBeInTheDocument();
    expect(screen.queryAllByTestId("dev-showcase-toast-example")).toHaveLength(0);
  });
});
