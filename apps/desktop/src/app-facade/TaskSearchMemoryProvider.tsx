import { useState, type ReactNode } from "react";
import { useAtomMount } from "@effect/atom-react";
import { createTaskSearchMemory, TaskSearchMemoryContext } from "./taskSearchMemoryContext";

export function TaskSearchMemoryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [model] = useState(createTaskSearchMemory);
  useAtomMount(model.state);
  return <TaskSearchMemoryContext.Provider value={model}>{children}</TaskSearchMemoryContext.Provider>;
}
