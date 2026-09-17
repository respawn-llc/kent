import { isCompacting, useChatMainViewState } from "@/app-facade";
import { ChatContextControl } from "./ChatContextControl";
import { ChatAutoCompactionSwitch } from "./ChatAutoCompactionSwitch";
import type { ChatSettingsFeature } from "./useChatSettings";

export function SessionChatContext({
  compact,
  settings,
}: Readonly<{ compact(): void; settings: ChatSettingsFeature }>) {
  const { data } = useChatMainViewState();
  return (
    <ChatContextControl
      used={data?.status.contextUsage.usedTokens ?? 0}
      window={data?.status.contextUsage.windowTokens ?? null}
      autoCompactionControl={<ChatAutoCompactionSwitch feature={settings} />}
      policyDisabled={data?.status.compactionMode === "disabled"}
      completedCount={data?.status.compactionCount ?? 0}
      compacting={isCompacting(data?.activity)}
      compact={compact}
    />
  );
}
