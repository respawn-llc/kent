import { isCompacting, useChatMainViewState } from "@/app-facade";
import { ChatContextControl } from "./ChatContextControl";
import { useAtomSet } from "@effect/atom-react";
import type { ChatMainView } from "@/api";
import { UsageContent, useSubscriptionUsageModel } from "@/shared/subscription-usage";

export function SessionChatContext({ compact }: Readonly<{ compact(): void }>) {
  const { data } = useChatMainViewState();
  if (data === undefined) return null;
  return <SessionContext data={data} compact={compact} />;
}

function SessionContext({ data, compact }: Readonly<{ data: ChatMainView; compact(): void }>) {
  const model = useSubscriptionUsageModel(data.sessionID);
  const refresh = useAtomSet(model.open);
  return (
    <ChatContextControl
      used={data.status.contextUsage.usedTokens}
      window={data.status.contextUsage.windowTokens}
      policyDisabled={data.status.compactionMode === "disabled"}
      compacting={isCompacting(data.activity)}
      compact={compact}
      onOpenChange={(open) => {
        if (open) refresh();
      }}
      subscriptionUsage={<UsageContent model={model} />}
    />
  );
}
