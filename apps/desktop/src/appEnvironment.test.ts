import { afterEach, vi } from "vitest";

import {
  applyNativeDialogThemeOverride,
  applyConfiguredTheme,
  installProductionContextMenuGuard,
  toggleInMemoryThemeOverride,
} from "./appEnvironment";

describe("applyConfiguredTheme", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.documentElement.removeAttribute("data-builder-theme");
    window.history.pushState(null, "", "/");
  });

  it("forces configured light and dark themes on the root element", () => {
    applyConfiguredTheme("light");
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");

    applyConfiguredTheme("dark");
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "dark");
  });

  it("keeps system theme when config is auto or invalid", () => {
    applyConfiguredTheme("dark");

    applyConfiguredTheme("auto");
    expect(document.documentElement).not.toHaveAttribute("data-builder-theme");

    applyConfiguredTheme("unknown");
    expect(document.documentElement).not.toHaveAttribute("data-builder-theme");
  });

  it("toggles an in-memory theme override without persistence", () => {
    applyConfiguredTheme("dark");

    expect(toggleInMemoryThemeOverride()).toBe("light");
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");

    expect(toggleInMemoryThemeOverride()).toBe("dark");
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "dark");
  });

  it("applies native dialog theme override from route search params only inside native dialogs", () => {
    applyConfiguredTheme("dark");
    window.history.pushState(null, "", "/native-dialog/new-task?__builderTheme=light");

    applyNativeDialogThemeOverride();

    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");

    window.history.pushState(null, "", "/projects/project-1?__builderTheme=dark");
    applyNativeDialogThemeOverride();

    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");
  });

  it("ignores invalid native dialog theme overrides", () => {
    applyConfiguredTheme("dark");
    window.history.pushState(null, "", "/native-dialog/new-task?__builderTheme=blue");

    applyNativeDialogThemeOverride();

    expect(document.documentElement).toHaveAttribute("data-builder-theme", "dark");
  });

  it("suppresses the default context menu in production only", () => {
    const addEventListener = vi.spyOn(document, "addEventListener");

    installProductionContextMenuGuard(false);

    expect(addEventListener).not.toHaveBeenCalled();

    installProductionContextMenuGuard(true);

    const registration = addEventListener.mock.calls.find(([type]) => type === "contextmenu");
    expect(registration).toBeDefined();
    const listener = registration?.[1];
    expect(listener).toBeTypeOf("function");
    const event = new MouseEvent("contextmenu", { cancelable: true });
    if (typeof listener !== "function") {
      throw new Error("contextmenu listener was not registered as a function");
    }
    listener(event);
    expect(event.defaultPrevented).toBe(true);
  });
});
