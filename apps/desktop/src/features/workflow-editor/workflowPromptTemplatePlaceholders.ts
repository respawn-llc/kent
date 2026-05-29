import type { DraftInputField } from "./workflowEditorDraft";
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
  inputFields: readonly Pick<DraftInputField, "name">[],
): readonly PromptTemplatePlaceholder[] {
  const seen = new Set<string>();
  const inputPlaceholders = inputFields.flatMap((field) => {
    const inputName = field.name.trim();
    if (!isWorkflowModelKeyValid(inputName)) {
      return [];
    }
    const value = `{{.Inputs.${inputName}}}`;
    if (seen.has(value)) {
      return [];
    }
    seen.add(value);
    return [{ label: `.Inputs.${inputName}`, tone: "primary" as const, value }];
  });
  return [
    ...inputPlaceholders,
    ...builtInPromptTemplatePlaceholderNames.map((name) => ({
      label: `.${name}`,
      tone: "muted" as const,
      value: `{{.${name}}}`,
    })),
  ];
}
