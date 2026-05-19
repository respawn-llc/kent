import { afterEach, vi } from "vitest";

import {
  applyNativeDialogThemeOverride,
  applyConfiguredTheme,
  createDefaultAppServices,
  installProductionContextMenuGuard,
  parseBrowserRpcEndpoint,
  readBrowserRpcEndpoint,
  toggleInMemoryThemeOverride,
} from "./appEnvironment";

describe("browser RPC endpoint configuration", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    window.history.pushState(null, "", "/");
  });

  it("defaults browser clients to the existing local Builder server", () => {
    expect(readBrowserRpcEndpoint()).toBe("ws://127.0.0.1:53082/rpc");
  });

  it("uses Vite env override for isolated browser QA environments", () => {
    vi.stubEnv("VITE_BUILDER_RPC_ENDPOINT", "ws://127.0.0.1:53100/rpc");

    expect(readBrowserRpcEndpoint()).toBe("ws://127.0.0.1:53100/rpc");
  });

  it("lets URL search params override env for one browser session", () => {
    vi.stubEnv("VITE_BUILDER_RPC_ENDPOINT", "ws://127.0.0.1:53100/rpc");
    window.history.pushState(null, "", "/?builderRpcEndpoint=ws%3A%2F%2F127.0.0.1%3A53101%2Frpc");

    expect(readBrowserRpcEndpoint()).toBe("ws://127.0.0.1:53101/rpc");
  });

  it("rejects non-websocket browser RPC endpoints", () => {
    const endpoint = parseBrowserRpcEndpoint("http://127.0.0.1:53082/rpc");

    expect(endpoint).toBeInstanceOf(Error);
    expect(endpoint).toHaveProperty("message", "Browser RPC endpoint must use ws:// or wss://.");
  });

  it("rejects browser RPC endpoints with credentials or fragments", () => {
    expect(parseBrowserRpcEndpoint("ws://user:pass@127.0.0.1:53082/rpc")).toBeInstanceOf(Error);
    expect(parseBrowserRpcEndpoint("ws://127.0.0.1:53082/rpc#debug")).toBeInstanceOf(Error);
  });

  it("creates default browser services against the existing local Builder server", async () => {
    const services = await createDefaultAppServices();

    expect(services.endpoint).toBe("ws://127.0.0.1:53082/rpc");
    await expect(services.nativeBridge.builder.resolvePlatform()).resolves.toBe("browser");
  });

  it("creates browser services with query override taking precedence over env override", async () => {
    vi.stubEnv("VITE_BUILDER_RPC_ENDPOINT", "ws://127.0.0.1:53100/rpc");
    window.history.pushState(null, "", "/?builderRpcEndpoint=ws%3A%2F%2F127.0.0.1%3A53101%2Frpc");

    const services = await createDefaultAppServices();

    expect(services.endpoint).toBe("ws://127.0.0.1:53101/rpc");
  });
});

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
