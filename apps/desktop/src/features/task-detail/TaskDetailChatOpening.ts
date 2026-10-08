import { createContext } from "react";
import type { SessionChatTarget } from "@/app-facade";

export const TaskDetailChatOpeningContext = createContext<SessionChatTarget | null>(null);
