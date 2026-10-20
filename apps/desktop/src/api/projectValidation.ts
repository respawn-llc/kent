import { create, validate, ValidationError, type Violation } from "@app/server-api-contract";
import { ProjectCatalogService } from "@app/server-api-contract/gen/kent/api/project/project_pb";

export function projectUpdateViolations(
  projectId: string,
  displayName: string,
  projectKey: string | undefined,
): readonly Violation[] {
  const schema = ProjectCatalogService.method.update.input;
  try {
    validate(schema, create(schema, { projectId, displayName, projectKey }));
    return [];
  } catch (error) {
    if (error instanceof ValidationError) return error.violations;
    throw error;
  }
}
