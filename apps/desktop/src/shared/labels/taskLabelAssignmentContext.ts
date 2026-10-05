import { createContext, useContext } from "react";

import { useTaskLabelAssignmentModel, type TaskLabelAssignmentModel } from "./taskLabelAssignmentData";

export const TaskLabelAssignmentContext = createContext<TaskLabelAssignmentModel | null>(null);

export function useTaskLabelAssignment() {
  const value = useContext(TaskLabelAssignmentContext);
  if (value === null) {
    throw new Error("TaskLabelAssignmentProvider is required");
  }
  return useTaskLabelAssignmentModel(value);
}
