import type { WorkflowParameter } from "../../api";
import { isWorkflowModelKeyValid } from "./workflowEditorGraphKeys";

export type PromptTemplatePlaceholderTone = "muted" | "primary";

export type PromptTemplatePlaceholder = Readonly<{
  label: string;
  tone: PromptTemplatePlaceholderTone;
  value: string;
}>;

// Keep this in sync with server/workflowrunner/starter.go nodePromptTemplateData.
export const builtInPromptTemplatePlaceholderNames = [
  "TaskId",
  "TaskShortId",
  "TaskTitle",
  "TaskBody",
  "NodeId",
  "NodeKey",
  "NodeDisplayName",
] as const;

export function workflowPromptTemplatePlaceholders(
  parameters: readonly Pick<WorkflowParameter, "key">[],
): readonly PromptTemplatePlaceholder[] {
  const seen = new Set<string>();
  const parameterPlaceholders = parameters.flatMap((parameter) => {
    const parameterKey = parameter.key.trim();
    if (!isWorkflowModelKeyValid(parameterKey)) {
      return [];
    }
    const value = `{{.Params.${parameterKey}}}`;
    if (seen.has(value)) {
      return [];
    }
    seen.add(value);
    return [{ label: `.Params.${parameterKey}`, tone: "primary" as const, value }];
  });
  return [
    ...parameterPlaceholders,
    ...builtInPromptTemplatePlaceholderNames.map((name) => ({
      label: `.${name}`,
      tone: "muted" as const,
      value: `{{.${name}}}`,
    })),
  ];
}
