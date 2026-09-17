import { create } from "@app/server-api-contract";
import { ProjectAvailability } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import { SessionExecutionTargetSchema } from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { executionFacts } from "./chatReadModel";
import { ContractError } from "./errors";

describe("execution target availability", () => {
  it.each([
    [ProjectAvailability.AVAILABLE, "available"],
    [ProjectAvailability.MISSING, "missing"],
    [ProjectAvailability.INACCESSIBLE, "inaccessible"],
    [ProjectAvailability.UNLINKED, "unlinked"],
  ] as const)("projects workspace and worktree availability %s", (availability, expected) => {
    const facts = executionFacts(
      create(SessionExecutionTargetSchema, {
        workspaceAvailability: availability,
        worktree: { availability },
      }),
    );

    expect(facts.WorkspaceAvailability).toBe(expected);
    expect(facts.Worktree?.Availability).toBe(expected);
  });

  it.each([ProjectAvailability.UNSPECIFIED, 999])("rejects invalid availability %s", (availability) => {
    expect(() =>
      executionFacts(create(SessionExecutionTargetSchema, { workspaceAvailability: availability })),
    ).toThrow(ContractError);
    expect(() =>
      executionFacts(
        create(SessionExecutionTargetSchema, {
          workspaceAvailability: ProjectAvailability.AVAILABLE,
          worktree: { availability },
        }),
      ),
    ).toThrow(ContractError);
  });
});
