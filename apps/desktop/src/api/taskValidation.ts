import { create, validate, ValidationError, type Violation } from "@app/server-api-contract";
import { TaskLifecycleService } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

export function taskCreateTitleViolations(projectId: string, title: string): readonly Violation[] {
  const schema = TaskLifecycleService.method.create.input;
  try {
    validate(schema, create(schema, { projectId, title }));
    return [];
  } catch (error) {
    if (error instanceof ValidationError) {
      return error.violations.filter((violation) => {
        const field = violation.field[0];
        return field?.kind === "field" && field.localName === "title";
      });
    }
    throw error;
  }
}
