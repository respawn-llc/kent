export function newTaskWindowOptions({
  projectID,
  title,
  workflowID,
}: Readonly<{
  projectID: string;
  title: string;
  workflowID: string;
}>) {
  return {
    initialHeight: 560,
    initialWidth: 608,
    label: `new-task-${projectID}-${Date.now().toString()}`,
    maximizable: true,
    params: {
      projectID,
      workflowID,
    },
    resizable: true,
    route: "/native-dialog/new-task",
    title,
  };
}
