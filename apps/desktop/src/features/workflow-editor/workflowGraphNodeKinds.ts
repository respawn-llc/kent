export function isInspectableWorkflowNodeKind(kind: string): boolean {
  return kind === "agent" || kind === "join" || kind === "start" || kind === "terminal";
}

export function hasWorkflowNodeMetadataTooltip(kind: string): boolean {
  return kind === "join";
}
