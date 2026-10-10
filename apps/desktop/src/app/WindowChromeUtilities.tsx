import { SunMoon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAppServices } from "@/app-facade";
import { toggleInMemoryThemeOverride } from "./startup/appEnvironment";
import { AppUpdateChip } from "./AppUpdateChip";
import { useDesktopUpdate } from "./useDesktopUpdate";

export function useWindowChromeUtilities() {
  const { t } = useTranslation();
  const { debugThemeOverrideEnabled, logger, nativeBridge } = useAppServices();
  const macOS = nativeBridge.capabilities.platform === "macos";
  const update = useDesktopUpdate(nativeBridge, logger);
  return {
    inlineUpdate: !macOS ? <AppUpdateChip state={update} /> : null,
    floatingUpdate: macOS ? (
      <div
        className="app-region-no-drag fixed top-[8px] right-[var(--space-4)] z-30 flex h-[22px] items-center"
        data-testid="app-chrome-update-slot"
      >
        <AppUpdateChip state={update} />
      </div>
    ) : null,
    themeToggle: debugThemeOverrideEnabled ? (
      <button
        aria-label={t("app.toggleTheme")}
        className="grid h-6 w-6 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)]"
        data-testid="app-chrome-debug-theme-toggle"
        onClick={() => {
          toggleInMemoryThemeOverride();
        }}
        type="button"
      >
        <SunMoon aria-hidden="true" size={16} strokeWidth={1.25} />
      </button>
    ) : null,
  };
}
