import { createBrowserNativeBridge } from "@builder/desktop-native-bridge";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach } from "vitest";

import { App } from "../App";
import { createTestServices, startupRoutes } from "../testSupport/appServices";

describe("AppChrome debug theme toggle", () => {
  afterEach(() => {
    document.documentElement.removeAttribute("data-builder-theme");
  });

  it("hides the in-memory theme toggle outside debug desktop builds", async () => {
    render(<App services={createTestServices(startupRoutes)} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Toggle theme")).not.toBeInTheDocument();
  });

  it("toggles the in-memory theme override in debug desktop builds", async () => {
    render(
      <App
        services={createTestServices(startupRoutes, createBrowserNativeBridge(), {
          debugThemeOverrideEnabled: true,
        })}
      />,
    );

    const toggle = await screen.findByLabelText("Toggle theme");
    expect(toggle).toHaveAttribute("data-testid", "app-chrome-debug-theme-toggle");

    fireEvent.click(toggle);
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");

    fireEvent.click(toggle);
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "dark");
  });
});
