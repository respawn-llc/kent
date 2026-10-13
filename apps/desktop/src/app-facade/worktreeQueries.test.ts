import { QueryClient } from "@tanstack/react-query";
import { newSetupOperationID } from "@/api";
import { worktreeQueryFixtureRoutes } from "@/test-support/api";
import { createTestServices } from "@/test-support/app-services";
import { queryKeys } from "./queryKeys";
import {
  createWorktreeSelectorRequest,
  createWorktreeTargetResolutionRequest,
  disposeWorktreeCreateTargetResolution,
  freshFetchWorktreeCreateTargetResolution,
  invalidateWorktreeSessionReads,
  worktreeCreateTargetResolutionQueryOptions,
  worktreeDeletePreviewQueryOptions,
  worktreeSelectorResolutionQueryOptions,
} from "./worktreeQueries";

describe("Worktree query ownership", () => {
  it("uses exact keys, fresh transient fetches, and durable-only invalidation", async () => {
    const services = createTestServices(worktreeQueryFixtureRoutes());
    const queryClient = new QueryClient();
    const request = createWorktreeTargetResolutionRequest(
      "33333333-3333-4333-8333-333333333333",
      " Feature ",
    );
    const key = queryKeys.worktreeCreateTargetResolution(request.sessionID, request.target);

    expect(key).toEqual([
      "worktree",
      "33333333-3333-4333-8333-333333333333",
      "create-target-resolution",
      "Feature",
    ]);
    await freshFetchWorktreeCreateTargetResolution(queryClient, services.api, request);
    await freshFetchWorktreeCreateTargetResolution(queryClient, services.api, request);
    expect(services.transport.descriptorCalls).toHaveLength(2);
    await disposeWorktreeCreateTargetResolution(queryClient, request);
    expect(queryClient.getQueryState(key)).toBeUndefined();
    const status = queryKeys.worktreeStatus("33333333-3333-4333-8333-333333333333");
    const list = queryKeys.worktreeList("33333333-3333-4333-8333-333333333333");
    const transient = queryKeys.worktreeDeletePreview("33333333-3333-4333-8333-333333333333", "feature");
    queryClient.setQueryData(status, {});
    queryClient.setQueryData(list, {});
    queryClient.setQueryData(transient, {});
    await invalidateWorktreeSessionReads(queryClient, "33333333-3333-4333-8333-333333333333");
    expect([status, list, transient].map((value) => queryClient.getQueryState(value)?.isInvalidated)).toEqual(
      [true, true, false],
    );
  });

  it("preserves changed authorities through QueryClient replacement", async () => {
    const services = createTestServices(worktreeQueryFixtureRoutes());
    const queryClient = new QueryClient();
    const createOptions = worktreeCreateTargetResolutionQueryOptions(
      services.api,
      createWorktreeTargetResolutionRequest("33333333-3333-4333-8333-333333333333", "feature"),
    );
    const selectorOptions = worktreeSelectorResolutionQueryOptions(
      services.api,
      createWorktreeSelectorRequest("33333333-3333-4333-8333-333333333333", "feature"),
    );
    const deleteOptions = worktreeDeletePreviewQueryOptions(
      services.api,
      createWorktreeSelectorRequest("33333333-3333-4333-8333-333333333333", "feature"),
    );

    await queryClient.fetchQuery(createOptions);
    await queryClient.fetchQuery(selectorOptions);
    await queryClient.fetchQuery(deleteOptions);
    const createTarget = await queryClient.fetchQuery(createOptions);
    const selector = await queryClient.fetchQuery(selectorOptions);
    const deletion = await queryClient.fetchQuery(deleteOptions);
    if (createTarget.resolution === undefined) {
      throw new Error("fixture omitted Create target resolution");
    }
    await services.api
      .createWorktree({
        sessionID: "33333333-3333-4333-8333-333333333333",
        setupOperationID: newSetupOperationID(),
        resolution: createTarget.resolution,
        baseRef: null,
      })
      .catch(() => undefined);
    const operation = selector.worktree?.projection?.switch;
    if (operation === undefined) throw new Error("fixture omitted Switch authority");
    await services.api
      .switchWorktree("33333333-3333-4333-8333-333333333333", operation)
      .catch(() => undefined);
    await services.api
      .deleteWorktree("33333333-3333-4333-8333-333333333333", deletion, "confirm")
      .catch(() => undefined);
    const expectedSpec: unknown = expect.objectContaining({ baseRef: "refs/heads/feature-1" });
    const expectedWorkspace: unknown = expect.objectContaining({
      workspaceId: "workspace-1",
      workspaceRoot: "/repo",
    });
    expect(services.transport.descriptorCalls.map(({ request }) => request)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          spec: expectedSpec,
        }),
        expect.objectContaining({
          selector: "feature-1",
          targetWorkspace: expectedWorkspace,
        }),
        expect.objectContaining({ forceFolderRemoval: true }),
      ]),
    );
  });
});
