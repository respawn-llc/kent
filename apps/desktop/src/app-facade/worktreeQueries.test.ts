import { describe, expect, it, vi } from "bun:test";
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
      { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" },
      " Feature ",
    );
    const key = worktreeCreateTargetResolutionQueryOptions(services.api, request).queryKey;
    await freshFetchWorktreeCreateTargetResolution(queryClient, services.api, request);
    await freshFetchWorktreeCreateTargetResolution(queryClient, services.api, request);
    expect(services.transport.descriptorCalls).toHaveLength(2);
    await disposeWorktreeCreateTargetResolution(queryClient, request);
    expect(queryClient.getQueryState(key)).toBeUndefined();
    const status = queryKeys.worktreeStatus("33333333-3333-4333-8333-333333333333");
    const scope = { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" } as const;
    const list = queryKeys.worktreeList(scope);
    const transient = worktreeDeletePreviewQueryOptions(services.api, {
      scope,
      selector: "feature",
    }).queryKey;
    queryClient.setQueryData(status, {});
    queryClient.setQueryData(list, {});
    await queryClient.fetchQuery(
      worktreeDeletePreviewQueryOptions(services.api, { scope, selector: "feature" }),
    );
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
      createWorktreeTargetResolutionRequest(
        { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" },
        "feature",
      ),
    );
    const selectorOptions = worktreeSelectorResolutionQueryOptions(
      services.api,
      createWorktreeSelectorRequest("33333333-3333-4333-8333-333333333333", "feature"),
    );
    const deleteOptions = worktreeDeletePreviewQueryOptions(services.api, {
      scope: { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" },
      selector: "feature",
    });

    await queryClient.fetchQuery(createOptions);
    await queryClient.fetchQuery(selectorOptions);
    await queryClient.fetchQuery(deleteOptions);
    const createTarget = await queryClient.fetchQuery(createOptions);
    const selector = await queryClient.fetchQuery(selectorOptions);
    const deletion = await queryClient.fetchQuery(deleteOptions);
    if (createTarget.resolution === undefined) {
      throw new Error("fixture omitted Create target resolution");
    }
    const createWorktree = vi.spyOn(services.api, "createWorktree");
    const switchWorktree = vi.spyOn(services.api, "switchWorktree");
    const deleteWorktree = vi.spyOn(services.api, "deleteWorktree");
    const setupOperationID = newSetupOperationID();
    await services.api
      .createWorktree({
        sessionID: "33333333-3333-4333-8333-333333333333",
        setupOperationID,
        resolution: createTarget.resolution,
        baseRef: null,
      })
      .catch(() => undefined);
    const operation = selector.worktree.projection.switch;
    if (operation === undefined) throw new Error("fixture omitted Switch authority");
    await services.api
      .switchWorktree("33333333-3333-4333-8333-333333333333", operation)
      .catch(() => undefined);
    await services.api
      .deleteWorktree(
        { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" },
        deletion,
        "confirm",
      )
      .catch(() => undefined);
    expect(createWorktree).toHaveBeenCalledWith({
      sessionID: "33333333-3333-4333-8333-333333333333",
      setupOperationID,
      resolution: createTarget.resolution,
      baseRef: null,
    });
    expect(switchWorktree).toHaveBeenCalledWith("33333333-3333-4333-8333-333333333333", operation);
    expect(deleteWorktree).toHaveBeenCalledWith(
      { kind: "session", sessionID: "33333333-3333-4333-8333-333333333333" },
      deletion,
      "confirm",
    );
  });
});
