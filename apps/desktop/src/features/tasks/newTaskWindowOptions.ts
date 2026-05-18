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
    initialWidth: 680,
    label: `new-task-${projectID}-${Date.now().toString()}`,
    params: {
      projectID,
      workflowID,
    },
    route: "/native-dialog/new-task",
    title,
  };
}
