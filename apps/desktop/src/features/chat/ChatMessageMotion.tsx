import { createContext, useContext, type ReactNode } from "react";
import type { SharedTextMotion } from "@/ui";

const MessageMotion = createContext<SharedTextMotion | null>(null);

export function ChatMessageMotion({
  children,
  motion,
}: Readonly<{ children: ReactNode; motion: SharedTextMotion }>) {
  return <MessageMotion.Provider value={motion}>{children}</MessageMotion.Provider>;
}

export function useChatMessageMotion() {
  const motion = useContext(MessageMotion);
  if (motion === null) throw new Error("Chat message motion requires the composer surface.");
  return motion;
}
