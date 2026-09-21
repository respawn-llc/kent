import { isCompacting, useChatMainViewState } from "@/app-facade";
import { ChatContextControl } from "./ChatContextControl";
import { ChatAutoCompactionSwitch } from "./ChatAutoCompactionSwitch";
import type { ChatSettingsFeature } from "./useChatSettings";

export function SessionChatContext({
  compact,
  settings,
}: Readonly<{ compact(): void; settings: ChatSettingsFeature }>) {
  const { data } = useChatMainViewState();
  if (data === undefined) return null;
  return (
    <ChatContextControl
      used={data.status.contextUsage.usedTokens}
      window={data.status.contextUsage.windowTokens}
      autoCompactionControl={<ChatAutoCompactionSwitch feature={settings} />}
      policyDisabled={data.status.compactionMode === "disabled"}
      completedCount={data.status.compactionCount}
      compacting={isCompacting(data.activity)}
      compact={compact}
    />
  );
}
