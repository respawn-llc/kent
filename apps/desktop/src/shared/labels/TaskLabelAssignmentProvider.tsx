import { useMemo, type ReactNode } from "react";
import { useAtomMount } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useAppServices } from "@/app-facade";

import { TaskLabelAssignmentContext } from "./taskLabelAssignmentContext";
import { createTaskLabelAssignmentModel } from "./taskLabelAssignmentData";
import { useProjectLabelData } from "./projectLabelContext";

export function TaskLabelAssignmentProvider({
  children,
  taskID,
}: Readonly<{
  children: ReactNode;
  taskID: string;
}>) {
  const { catalog, effects, projectID } = useProjectLabelData();
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(
    () =>
      createTaskLabelAssignmentModel({
        api,
        catalog,
        client,
        projectID,
        taskID,
        scheduleCatalogRefresh: effects.scheduleCatalogRefresh,
        scheduleTaskAssignmentRefresh: effects.scheduleTaskAssignmentRefresh,
      }),
    [api, catalog, client, projectID, taskID, effects],
  );
  useAtomMount(model.observation);
  return <TaskLabelAssignmentContext.Provider value={model}>{children}</TaskLabelAssignmentContext.Provider>;
}
