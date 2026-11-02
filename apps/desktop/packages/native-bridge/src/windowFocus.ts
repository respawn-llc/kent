import { getCurrentWindow, type Window as TauriWindow } from "@tauri-apps/api/window";
import { invoke } from "@tauri-apps/api/core";
import type * as Stream from "effect/Stream";
import { nativeObservation, type NativeOverflowReporter } from "./observation";

export type NativeWindowFocusControls = Readonly<{
  isFocused(): Promise<boolean>;
  focusMain(): Promise<void>;
  focusChanges(reportOverflow: NativeOverflowReporter): Stream.Stream<boolean, Error>;
}>;

export function createBrowserWindowFocusControls(): NativeWindowFocusControls {
  return {
    async isFocused(): Promise<boolean> {
      return typeof document !== "undefined" && document.hasFocus();
    },
    async focusMain(): Promise<void> {
      if (typeof window !== "undefined") {
        window.focus();
      }
    },
    focusChanges: (reportOverflow) =>
      nativeObservation(async (handler) => {
        if (typeof window === "undefined") {
          return () => undefined;
        }
        const notifyFocused = (): void => {
          handler(true);
        };
        const notifyBlurred = (): void => {
          handler(false);
        };
        window.addEventListener("focus", notifyFocused);
        window.addEventListener("blur", notifyBlurred);
        return () => {
          window.removeEventListener("focus", notifyFocused);
          window.removeEventListener("blur", notifyBlurred);
        };
      }, reportOverflow),
  };
}

export function createTauriWindowFocusControls(
  getWindow: () => TauriWindow = getCurrentWindow,
): NativeWindowFocusControls {
  return {
    async isFocused(): Promise<boolean> {
      return getWindow().isFocused();
    },
    async focusMain(): Promise<void> {
      await invoke("open_main_window", { destination: null });
    },
    focusChanges: (reportOverflow) =>
      nativeObservation(async (handler) => {
        return getWindow().onFocusChanged((event) => {
          handler(event.payload);
        });
      }, reportOverflow),
  };
}
