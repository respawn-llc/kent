import { ChatDestination } from "./ChatDestination";
import type { ChatDestinationNavigation } from "./useChatSettings";

export function NewChatDestination({
  projectID,
  navigation,
  onSessionDelivered,
  onPopOutCreated,
}: Readonly<{
  projectID: string;
  navigation: ChatDestinationNavigation;
  onSessionDelivered(sessionID: string): void;
  onPopOutCreated?: ((projectID: string) => Promise<void>) | undefined;
}>) {
  return (
    <ChatDestination
      opening={{ kind: "new_chat", projectID, workspace: null }}
      navigation={navigation}
      onSessionDelivered={onSessionDelivered}
      {...(onPopOutCreated === undefined ? {} : { onPopOutCreated })}
    />
  );
}
