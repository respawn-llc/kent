import { useTranslation } from "react-i18next";
import { errorMessage } from "@/api";
import { Spinner } from "@/ui";
import type { ChatSettingsFeature, ReadyChatSettings } from "./useChatSettings";
import {
  activateChatSetting,
  settingsDisabledReason,
  settingsPresentation,
} from "./chatSettingsPresentation";
import { SettingsSwitch } from "./ChatSettingsRows";

export function ChatAutoCompactionSwitch({
  feature,
}: Readonly<{ feature: ChatSettingsFeature | ReadyChatSettings }>) {
  const { t } = useTranslation();
  if (feature.kind === "failed-session" || feature.kind === "failed-new-chat")
    return <span>{errorMessage(feature.error)}</span>;
  if (feature.kind === "loading-session" || feature.kind === "loading-new-chat")
    return (
      <span className="flex items-center gap-[var(--space-1)]">
        <Spinner size="sm" />
        {t("chatComposer.loadingSettings")}
      </span>
    );
  const { autoCompaction } = settingsPresentation(feature);
  return (
    <SettingsSwitch
      checked={autoCompaction.policy === "disabled" ? autoCompaction.stored : autoCompaction.effective}
      label={t("chatSettings.autoCompaction")}
      onChange={(enabled) => {
        activateChatSetting(feature, { kind: "auto_compaction", enabled });
      }}
      reason={settingsDisabledReason(t, autoCompaction.editability, autoCompaction.policy)}
    />
  );
}
