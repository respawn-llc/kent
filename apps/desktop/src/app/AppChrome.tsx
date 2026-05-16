import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Island } from "../ui";
import { useConnectionSnapshot } from "./useConnectionSnapshot";

export type AppChromeProps = Readonly<{
  children: ReactNode;
}>;

export function AppChrome({ children }: AppChromeProps) {
  const { t } = useTranslation();
  const connection = useConnectionSnapshot();

  return (
    <main className="app-shell">
      <header className="app-chrome">
        <div className="app-chrome__brand">
          <div className="app-logo" aria-hidden="true" />
          <div>
            <p>{t("app.title")}</p>
            <span>{t("app.subtitle")}</span>
          </div>
        </div>
        <nav className="app-chrome__nav">
          <Link className="ui-button ui-button--ghost" to="/">{t("app.home")}</Link>
          <Link className="ui-button ui-button--ghost" to="/settings">{t("app.settings")}</Link>
        </nav>
        <Badge tone={connection.phase === "connected" ? "success" : "warning"}>
          {connection.phase === "connected" ? t("app.connected") : t("app.reconnecting")}
        </Badge>
      </header>
      <Island className="app-surface" tone="primary">
        {children}
      </Island>
    </main>
  );
}
