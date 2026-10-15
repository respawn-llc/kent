import { create, validate } from "@app/server-api-contract";
import { CreateSuccessSchema } from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";

describe("Worktree Create result", () => {
  it("requires registered topology", () => {
    const git = { canonicalRoot: "/repo/feature", headObject: "abc123" };
    const registered = create(CreateSuccessSchema, {
      worktree: {
        topology: {
          topology: {
            case: "registered",
            value: {
              git,
              kent: {
                worktreeId: "worktree-1",
                canonicalRoot: "/repo/feature",
                displayName: "feature",
              },
            },
          },
        },
        projection: { selector: "feature" },
      },
    });
    expect(() => validate(CreateSuccessSchema, registered)).not.toThrow();
    const main = create(CreateSuccessSchema, {
      worktree: {
        topology: { topology: { case: "mainWorkspace", value: { git } } },
        projection: { selector: "feature" },
      },
    });
    expect(() => validate(CreateSuccessSchema, main)).toThrow();
  });
});
