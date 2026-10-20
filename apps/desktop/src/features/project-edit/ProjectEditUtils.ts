import { projectUpdateViolations } from "@/api";

const ruleMessages: Readonly<Record<string, readonly string[]>> = {
  "string.min_len": ["form.projectNameLength"],
  "string.max_len": ["form.projectNameLength"],
  "update_project.display_name": ["form.noEdgeWhitespace", "form.singleLine"],
  "kent.project.update.project_key": ["form.projectKeyStartsWithLetter", "form.projectKeySymbols"],
};

export function projectDraftErrors(
  projectID: string,
  name: string,
  key: string | undefined,
  t: (key: string) => string,
): Readonly<{ nameErrors: readonly string[]; keyErrors: readonly string[] }> {
  const nameErrors = new Set<string>();
  const keyErrors = new Set<string>();
  for (const violation of projectUpdateViolations(projectID, name, key)) {
    const field = violation.field[0];
    if (field?.kind !== "field") throw new Error("Unexpected Project update violation path");
    const target = field.localName === "displayName" ? nameErrors : keyErrors;
    const messages =
      field.localName === "projectKey" &&
      (violation.ruleId === "string.min_len" || violation.ruleId === "string.max_len")
        ? ["form.projectKeyLength"]
        : ruleMessages[violation.ruleId];
    if (messages === undefined) throw new Error(`Unexpected Project update rule: ${violation.ruleId}`);
    for (const message of messages) target.add(t(message));
  }
  return { nameErrors: [...nameErrors], keyErrors: [...keyErrors] };
}
