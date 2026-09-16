import { isCompacting, useChatMainViewState } from "@/app-facade";
import { ChatContextControl } from "./ChatContextControl";

export function SessionChatContext({ compact }: Readonly<{ compact(): void }>) {
  const { data } = useChatMainViewState();
  return (
    <ChatContextControl
      used={data?.status.contextUsage.usedTokens ?? 0}
      window={data?.status.contextUsage.windowTokens ?? null}
      autoCompactionEnabled={data?.status.autoCompactionEnabled ?? false}
      policyDisabled={data?.status.compactionMode === "disabled"}
      completedCount={data?.status.compactionCount ?? 0}
      compacting={isCompacting(data?.activity)}
      compact={compact}
    />
  );
}
