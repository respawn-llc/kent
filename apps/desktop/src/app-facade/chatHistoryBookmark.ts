import { useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { sessionChatHistoryStateSchema } from "./sessionChatHistory";

export function useChatHistoryBookmark(projectID: string, routeSessionID: string | null) {
  const { history } = useRouter();
  const [opening] = useState(() => {
    const metadata = sessionChatHistoryStateSchema.parse(history.location.state).sessionChat;
    return {
      href: history.location.href,
      index: history.location.state.__TSR_index,
      key: history.location.state.__TSR_key,
      sessionID:
        metadata?.projectID === projectID ? (metadata.deliveredSessionID ?? routeSessionID) : routeSessionID,
    };
  });
  return {
    sessionID: opening.sessionID,
    delivered: (sessionID: string) => {
      const location = history.location;
      if (location.href !== opening.href || location.state.__TSR_index !== opening.index) return;
      const state = sessionChatHistoryStateSchema.parse(window.history.state);
      if (state.__TSR_key !== opening.key) return;
      const metadata = state.sessionChat;
      // Router patches the instance method to navigate. This metadata-only write
      // preserves its entry identity and is read normally on history revisit.
      window.History.prototype.replaceState.call(
        window.history,
        {
          ...state,
          sessionChat: {
            projectID,
            catalogOrigin: metadata?.catalogOrigin ?? null,
            deliveredSessionID: sessionID,
          },
        },
        "",
        location.href,
      );
    },
  };
}
