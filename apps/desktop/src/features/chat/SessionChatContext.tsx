import { isCompacting, useChatMainViewState } from "@/app-facade";
import { ChatContextControl } from "./ChatContextControl";

export function SessionChatContext({ compact }: Readonly<{ compact(): void }>) {
  const { data } = useChatMainViewState();
  if (data === undefined) return null;
  return (
    <ChatContextControl
      used={data.status.contextUsage.usedTokens}
      window={data.status.contextUsage.windowTokens}
      policyDisabled={data.status.compactionMode === "disabled"}
      compacting={isCompacting(data.activity)}
      compact={compact}
    />
  );
}
