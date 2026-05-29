import {
  createBrowserNativeBridge,
  createTauriNativeBridge,
  taskDetailContentMaxWidthPx,
  taskDetailDialogHorizontalPaddingPx,
  taskDetailNativeDialogWindowOptions,
  taskDetailNativeWindowInitialWidthPx,
} from "@builder/desktop-native-bridge";
import { vi } from "vitest";

import tauriDefaultCapability from "../src-tauri/capabilities/default.json";

describe("native bridge capabilities", () => {
  it("keeps browser fallback capabilities disabled and explicit", async () => {
    const bridge = createBrowserNativeBridge();

    expect(bridge.capabilities.platform).toBe("browser");
    expect(bridge.capabilities.clipboard).toEqual({ readText: false, writeText: false });
    expect(bridge.capabilities.directories.select).toBe(false);
    expect(bridge.capabilities.links.openExternal).toBe(false);
    expect(bridge.capabilities.dialogWindows).toBe(false);
    expect(bridge.capabilities.projectCreationWindow).toBe(false);
    expect(bridge.capabilities.taskDetailWindow).toBe(false);
    await expect(bridge.builder.resolvePlatform()).resolves.toBe("browser");
    await expect(createBrowserNativeBridge({ platform: "macos" }).builder.resolvePlatform()).resolves.toBe(
      "macos",
    );
    await expect(bridge.clipboard.readText()).rejects.toThrow("Native clipboard is unavailable");
  });

  it("advertises Tauri capabilities only for implemented bridge methods", () => {
    const bridge = createTauriNativeBridge("macos");

    expect(bridge.capabilities.platform).toBe("macos");
    expect(bridge.capabilities.clipboard).toEqual({ readText: true, writeText: true });
    expect(bridge.capabilities.directories.select).toBe(true);
    expect(bridge.capabilities.links.openExternal).toBe(true);
    expect(bridge.capabilities.logging.localFile).toBe(true);
    expect(bridge.capabilities.windowDrag).toBe(true);
    expect(bridge.capabilities.dialogWindows).toBe(true);
    expect(bridge.capabilities.projectCreationWindow).toBe(true);
    expect(bridge.capabilities.taskDetailWindow).toBe(true);
    expect(bridge.capabilities.notifications.basic).toBe(false);
    expect(bridge.capabilities.tray).toBe(false);
    expect(bridge.capabilities.appMenu).toBe(false);
    expect(bridge.capabilities.updater).toBe(false);
    expect(bridge.capabilities.macosVibrancy).toBe(false);
  });

  it("keeps Tauri permissions aligned with bridge event and window APIs", () => {
    const permissions = new Set(tauriDefaultCapability.permissions);

    expect(tauriDefaultCapability.windows).toContain("native-dialog-*");
    [
      "clipboard-manager:allow-read-text",
      "clipboard-manager:allow-write-text",
      "core:event:allow-emit",
      "core:event:allow-emit-to",
      "core:event:allow-listen",
      "core:event:allow-unlisten",
      "core:webview:allow-create-webview-window",
      "core:window:allow-close",
      "core:window:allow-get-all-windows",
      "core:window:allow-set-focus",
      "core:window:allow-set-max-size",
      "core:window:allow-set-min-size",
      "core:window:allow-set-size",
      "core:window:allow-start-dragging",
    ].forEach((permission) => {
      expect(permissions.has(permission)).toBe(true);
    });
  });

  it("uses the compact task detail native dialog default width", () => {
    expect(taskDetailContentMaxWidthPx).toBe(1200);
    expect(taskDetailDialogHorizontalPaddingPx).toBe(16);
    expect(taskDetailNativeWindowInitialWidthPx).toBe(840);
    expect(taskDetailNativeDialogWindowOptions({ resumeRunId: "run-1", taskId: "task-1" })).toMatchObject({
      initialHeight: 760,
      initialWidth: 840,
      params: { resumeRunId: "run-1", taskId: "task-1" },
      route: "/native-dialog/task-detail",
      title: "Task",
    });
  });

  it("keeps browser workflow delete confirmation events as no-ops", async () => {
    const bridge = createBrowserNativeBridge();
    const handler = vi.fn();

    const unlisten = await bridge.workflowEditor.onGraphDeleteConfirmed(handler);
    await bridge.workflowEditor.confirmGraphDelete({ requestID: "delete-1" });
    unlisten();

    expect(handler).not.toHaveBeenCalled();
  });
});
