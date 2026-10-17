import type { SessionChatOpeningIntent, SessionChatTarget } from "@/app-facade";

export type TaskDetailSessionChatEntry = (
  target: SessionChatTarget,
  intent?: SessionChatOpeningIntent,
) => Promise<void>;
