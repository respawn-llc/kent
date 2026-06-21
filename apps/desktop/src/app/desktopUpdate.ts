import type { NativeBridge, NativeUpdateDownloadProgress } from "@app/native-bridge";

import type { GuiLogger } from "./logging";

export type DesktopUpdateAvailability = Readonly<{
  available: boolean;
  version: string;
}>;

// Checks for an available update, guarding on the updater capability and the
// install-local self-update setting, and swallowing transient check failures. A
// failed check stays silent (no error chip): the next launch retries. Returns
// available:false when the shell can't update, self-update is disabled (e.g. a
// Homebrew install, where brew owns updates), or the check failed.
export async function checkForDesktopUpdate(
  nativeBridge: NativeBridge,
  logger: GuiLogger,
): Promise<DesktopUpdateAvailability> {
  if (!nativeBridge.capabilities.updater) {
    return { available: false, version: "" };
  }
  if (await isSelfUpdateDisabled(nativeBridge, logger)) {
    return { available: false, version: "" };
  }
  try {
    const result = await nativeBridge.updates.check();
    return { available: result.available, version: result.version };
  } catch (error) {
    await logger.append("warn", "Desktop update check failed.", { error: errorText(error) });
    return { available: false, version: "" };
  }
}

export type DesktopUpdateInstall = Readonly<{ ok: boolean }>;

// Downloads + installs the pending update (reporting download progress), then
// relaunches into the new version. Relaunch only runs after a clean install.
// Returns ok:false and logs on failure so the caller can surface a retryable
// error instead of leaving the app in a half-updated state.
export async function installDesktopUpdate(
  nativeBridge: NativeBridge,
  logger: GuiLogger,
  onProgress: (progress: NativeUpdateDownloadProgress) => void,
): Promise<DesktopUpdateInstall> {
  try {
    await nativeBridge.updates.downloadAndInstall(onProgress);
  } catch (error) {
    await logger.append("error", "Desktop update download/install failed.", {
      error: errorText(error),
    });
    return { ok: false };
  }
  try {
    await nativeBridge.updates.relaunch();
  } catch (error) {
    await logger.append("error", "Desktop update relaunch failed.", { error: errorText(error) });
    return { ok: false };
  }
  return { ok: true };
}

// Reads the install-local self-update gate. A read failure is treated as "not
// disabled" so a missing/corrupt settings file never silently suppresses updates
// for direct-download installs; the cask explicitly writes "disabled" for brew.
async function isSelfUpdateDisabled(nativeBridge: NativeBridge, logger: GuiLogger): Promise<boolean> {
  try {
    const settings = await nativeBridge.settings.read();
    return settings.selfUpdate === "disabled";
  } catch (error) {
    await logger.append("warn", "Desktop settings read failed.", { error: errorText(error) });
    return false;
  }
}

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  return "Unknown desktop update error.";
}
