import { useMemo, type ReactNode } from "react";
import { useAtomValue } from "@effect/atom-react";

import { TaskLabelAssignmentContext } from "./taskLabelAssignmentContext";
import { useManagedTaskLabelAssignment } from "./taskLabelAssignmentData";
import { useProjectLabelData } from "./projectLabelContext";

export function TaskLabelAssignmentProvider({
  children,
  taskID,
}: Readonly<{
  children: ReactNode;
  taskID: string;
}>) {
  const { catalog: observation, effects, projectID } = useProjectLabelData();
  const catalog = useAtomValue(observation);
  const availableLabelIDs = useMemo(
    () => catalog.data?.labels.map((label) => label.id) ?? [],
    [catalog.data],
  );
  const assignment = useManagedTaskLabelAssignment({
    availableLabelIDs,
    projectID,
    scheduleCatalogRefresh: effects.scheduleCatalogRefresh,
    scheduleTaskAssignmentRefresh: effects.scheduleTaskAssignmentRefresh,
    taskID,
  });
  return (
    <TaskLabelAssignmentContext.Provider value={assignment}>{children}</TaskLabelAssignmentContext.Provider>
  );
}
