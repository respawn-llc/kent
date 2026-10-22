import { nativeObservation } from "@app/native-bridge";

export function themeChanges(reportOverflow: () => Promise<void>) {
  return nativeObservation<undefined>(async (emit) => {
    const observer = new MutationObserver(() => {
      emit(undefined);
    });
    observer.observe(document.documentElement, {
      attributeFilter: ["data-theme"],
      attributes: true,
    });
    const systemTheme =
      window.matchMedia instanceof Function ? window.matchMedia("(prefers-color-scheme: light)") : null;
    const changed = () => {
      emit(undefined);
    };
    systemTheme?.addEventListener("change", changed);
    emit(undefined);
    return () => {
      observer.disconnect();
      systemTheme?.removeEventListener("change", changed);
    };
  }, reportOverflow);
}
