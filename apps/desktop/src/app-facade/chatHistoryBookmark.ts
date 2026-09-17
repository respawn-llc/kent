import { useRouter } from "@tanstack/react-router";
import { useRef, useState } from "react";
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
  const expected = useRef({ href: opening.href, index: opening.index, key: opening.key });
  return {
    sessionID: opening.sessionID,
    delivered: (sessionID: string) => {
      const location = history.location;
      if (
        location.href !== expected.current.href ||
        location.state.__TSR_index !== expected.current.index ||
        location.state.__TSR_key !== expected.current.key
      )
        return;
      const state = sessionChatHistoryStateSchema.parse(location.state);
      const metadata = state.sessionChat;
      history.replace(
        location.href,
        {
          ...state,
          sessionChat: {
            projectID,
            catalogOrigin: metadata?.catalogOrigin ?? null,
            deliveredSessionID: sessionID,
          },
        },
        { ignoreBlocker: true },
      );
      const replaced = history.location;
      expected.current = {
        href: replaced.href,
        index: replaced.state.__TSR_index,
        key: replaced.state.__TSR_key,
      };
    },
  };
}
