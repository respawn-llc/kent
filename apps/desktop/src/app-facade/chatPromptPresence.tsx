import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import type { ChatSessionTarget } from "@/api";

type PickerTarget = Pick<ChatSessionTarget, "projectID" | "sessionID">;
type Presence = Readonly<{
  target: PickerTarget | null;
  register(target: PickerTarget): () => void;
}>;
const Context = createContext<Presence | null>(null);

export function ChatPromptPresenceProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [target, setTarget] = useState<PickerTarget | null>(null);
  const register = useCallback((next: PickerTarget) => {
    setTarget(next);
    return () => {
      setTarget((current) => (current === next ? null : current));
    };
  }, []);
  const value = useMemo(() => ({ target, register }), [target, register]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function useChatPromptPresence(): Presence {
  const value = useContext(Context);
  if (value === null) throw new Error("Chat prompt presence requires its provider.");
  return value;
}

export function usePublishChatPromptPresence(target: PickerTarget, visible: boolean): void {
  const { register } = useChatPromptPresence();
  useEffect(() => {
    if (!visible) return;
    return register({ projectID: target.projectID, sessionID: target.sessionID });
  }, [register, target.projectID, target.sessionID, visible]);
}
