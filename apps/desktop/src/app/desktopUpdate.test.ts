import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeUpdateAvailability,
  type NativeUpdateDownloadProgress,
} from "@app/native-bridge";
import { vi } from "vitest";

import { checkForDesktopUpdate, installDesktopUpdate } from "./desktopUpdate";
import type { GuiLogger } from "./logging";

const noUpdate: NativeUpdateAvailability = {
  available: false,
  version: "",
  currentVersion: "",
  notes: "",
  publishedAt: "",
};

function fakeLogger(): GuiLogger {
  return { entries: () => [], append: vi.fn(async () => undefined) };
}

function fakeBridge(updates: Partial<NativeBridge["updates"]>, updater = true): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: { ...base.capabilities, updater },
    updates: { ...base.updates, ...updates },
  };
}

describe("checkForDesktopUpdate", () => {
  it("skips the check when the shell cannot update", async () => {
    const check = vi.fn(async () => noUpdate);
    const bridge = fakeBridge({ check }, false);

    await expect(checkForDesktopUpdate(bridge, fakeLogger())).resolves.toEqual({
      available: false,
      version: "",
    });
    expect(check).not.toHaveBeenCalled();
  });

  it("reports an available update with its version", async () => {
    const bridge = fakeBridge({
      async check() {
        return { ...noUpdate, available: true, version: "2.2.0" };
      },
    });

    await expect(checkForDesktopUpdate(bridge, fakeLogger())).resolves.toEqual({
      available: true,
      version: "2.2.0",
    });
  });

  it("stays silent and logs when the check fails", async () => {
    const logger = fakeLogger();
    const bridge = fakeBridge({
      async check() {
        throw new Error("network down");
      },
    });

    await expect(checkForDesktopUpdate(bridge, logger)).resolves.toEqual({
      available: false,
      version: "",
    });
    expect(logger.append).toHaveBeenCalled();
  });
});

describe("installDesktopUpdate", () => {
  it("downloads, relaunches, and forwards progress", async () => {
    const order: string[] = [];
    const progress: NativeUpdateDownloadProgress[] = [];
    const bridge = fakeBridge({
      async downloadAndInstall(onProgress) {
        order.push("download");
        onProgress?.({ downloadedBytes: 50, totalBytes: 100 });
      },
      async relaunch() {
        order.push("relaunch");
      },
    });

    const result = await installDesktopUpdate(bridge, fakeLogger(), (p) => progress.push(p));

    expect(result).toEqual({ ok: true });
    expect(order).toEqual(["download", "relaunch"]);
    expect(progress).toEqual([{ downloadedBytes: 50, totalBytes: 100 }]);
  });

  it("does not relaunch when the download fails", async () => {
    const logger = fakeLogger();
    const relaunch = vi.fn(async () => undefined);
    const bridge = fakeBridge({
      async downloadAndInstall() {
        throw new Error("download failed");
      },
      relaunch,
    });

    const result = await installDesktopUpdate(bridge, logger, () => undefined);

    expect(result).toEqual({ ok: false });
    expect(relaunch).not.toHaveBeenCalled();
    expect(logger.append).toHaveBeenCalled();
  });

  it("reports failure when the relaunch fails", async () => {
    const logger = fakeLogger();
    const bridge = fakeBridge({
      async relaunch() {
        throw new Error("relaunch failed");
      },
    });

    const result = await installDesktopUpdate(bridge, logger, () => undefined);

    expect(result).toEqual({ ok: false });
    expect(logger.append).toHaveBeenCalled();
  });
});
