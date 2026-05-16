import { useTranslation } from "react-i18next";

import { useAppServices } from "../../app/useAppServices";
import { Badge, Island } from "../../ui";

export function SettingsRoute() {
  const { t } = useTranslation();
  const { endpoint, logger, nativeBridge } = useAppServices();
  const capabilities = nativeBridge.capabilities;

  return (
    <div className="settings-page">
      <header className="page-header">
        <div>
          <p className="eyebrow">{t("app.diagnostics")}</p>
          <h1>{t("settings.title")}</h1>
          <p>{t("settings.body")}</p>
        </div>
        <Badge tone="info">{endpoint}</Badge>
      </header>
      <section className="settings-grid">
        <Island tone="secondary">
          <h2>{t("settings.nativeCapabilities")}</h2>
          <ul className="capability-list">
            <Capability label={t("settings.clipboard")} value={capabilities.clipboard.writeText} />
            <Capability label={t("settings.directoryPicker")} value={capabilities.directories.select} />
            <Capability label={t("settings.terminal")} value={capabilities.terminal.launchBuilderSession} />
            <Capability label={t("settings.localLog")} value={capabilities.logging.localFile} />
          </ul>
        </Island>
        <Island tone="secondary">
          <h2>{t("settings.guiLog")}</h2>
          <pre className="log-preview">
            {logger
              .entries()
              .slice(-20)
              .map((entry) => `${entry.occurredAt} ${entry.level} ${entry.message}`)
              .join("\n")}
          </pre>
        </Island>
      </section>
    </div>
  );
}

type CapabilityProps = Readonly<{
  label: string;
  value: boolean;
}>;

function Capability({ label, value }: CapabilityProps) {
  const { t } = useTranslation();
  return (
    <li>
      <span>{label}</span>
      <Badge tone={value ? "success" : "warning"}>{value ? t("app.available") : t("app.unavailable")}</Badge>
    </li>
  );
}
