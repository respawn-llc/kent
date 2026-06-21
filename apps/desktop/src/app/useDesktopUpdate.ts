import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { NativeBridge } from "@app/native-bridge";

import { checkForDesktopUpdate, installDesktopUpdate } from "./desktopUpdate";
import type { GuiLogger } from "./logging";

export type DesktopUpdatePhase = "none" | "available" | "installing" | "error";

export type DesktopUpdateState = Readonly<{
  phase: DesktopUpdatePhase;
  version: string;
  // Download ratio 0..1 while installing, or null when the total size is unknown
  // or no download is in progress.
  progressRatio: number | null;
  install: () => void;
  // Hides the chip for this session; it returns on the next launch because the
  // check re-runs on mount (no update is permanently lost).
  dismiss: () => void;
}>;

type DesktopUpdatePreview = Readonly<{
  phase: DesktopUpdatePhase;
  version: string;
  progressRatio: number | null;
}>;

// DEV-only chip preview. `?__updatePreview=available|installing|error` forces the
// chip into a state so the update UX can be exercised in `dev:browser` without a
// published release. Compiled out of production (import.meta.env.DEV is false).
function readDesktopUpdatePreview(): DesktopUpdatePreview | null {
  if (!import.meta.env.DEV || typeof window === "undefined") {
    return null;
  }
  const value = new URLSearchParams(window.location.search).get("__updatePreview");
  if (value === "available" || value === "error") {
    return { phase: value, version: "2.2.0", progressRatio: null };
  }
  if (value === "installing") {
    return { phase: "installing", version: "2.2.0", progressRatio: 0.6 };
  }
  return null;
}

// Drives the app-chrome update chip: checks once on mount (auto), exposes the
// available version, runs a one-shot download -> install -> relaunch on install(),
// and hides the chip for the session on dismiss(). On a successful install the app
// relaunches, so there is no terminal "done" state; failures fall back to "error"
// so the chip stays actionable.
export function useDesktopUpdate(nativeBridge: NativeBridge, logger: GuiLogger): DesktopUpdateState {
  const preview = useMemo(() => readDesktopUpdatePreview(), []);
  const [phase, setPhase] = useState<DesktopUpdatePhase>(preview?.phase ?? "none");
  const [version, setVersion] = useState(preview?.version ?? "");
  const [progressRatio, setProgressRatio] = useState<number | null>(preview?.progressRatio ?? null);
  const installingRef = useRef(false);

  useEffect(() => {
    if (preview !== null) {
      return undefined;
    }
    let cancelled = false;
    void checkForDesktopUpdate(nativeBridge, logger).then((result) => {
      if (cancelled || !result.available) {
        return;
      }
      setVersion(result.version);
      setPhase("available");
    });
    return () => {
      cancelled = true;
    };
  }, [preview, nativeBridge, logger]);

  const dismiss = useCallback(() => {
    setPhase("none");
  }, []);

  const install = useCallback(() => {
    if (preview !== null) {
      setProgressRatio(0.6);
      setPhase("installing");
      return;
    }
    if (installingRef.current) {
      return;
    }
    installingRef.current = true;
    setProgressRatio(null);
    setPhase("installing");
    void installDesktopUpdate(nativeBridge, logger, (progress) => {
      setProgressRatio(
        progress.totalBytes !== null && progress.totalBytes > 0
          ? progress.downloadedBytes / progress.totalBytes
          : null,
      );
    }).then((result) => {
      installingRef.current = false;
      if (!result.ok) {
        setPhase("error");
      }
      // On success the app relaunches into the new version; no state change here.
    });
  }, [preview, nativeBridge, logger]);

  return { phase, version, progressRatio, install, dismiss };
}
