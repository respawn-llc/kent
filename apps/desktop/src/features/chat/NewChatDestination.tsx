import { ChatDestination } from "./ChatDestination";
import type { ChatSettingsNavigation } from "./useChatSettings";

export function NewChatDestination({
  projectID,
  navigation,
  onSessionDelivered,
}: Readonly<{
  projectID: string;
  navigation: ChatSettingsNavigation;
  onSessionDelivered(sessionID: string): void;
}>) {
  return (
    <ChatDestination
      opening={{ kind: "new_chat", projectID, workspace: null }}
      navigation={navigation}
      onSessionDelivered={onSessionDelivered}
    />
  );
}
