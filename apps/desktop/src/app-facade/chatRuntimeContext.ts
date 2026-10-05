import { createContext } from "react";

import type { ChatRuntimeOwner } from "./chatRuntime";

export const ChatRuntimeContext = createContext<ChatRuntimeOwner | null>(null);
