import { SunMoon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAppServices } from "@/app-facade";
import { toggleInMemoryThemeOverride } from "./startup/appEnvironment";

export function WindowChromeThemeToggle() {
  const { t } = useTranslation();
  const { debugThemeOverrideEnabled } = useAppServices();
  return debugThemeOverrideEnabled ? (
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
  ) : null;
}
