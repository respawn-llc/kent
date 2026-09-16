import { useAtomRefresh, useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useMemo } from "react";
import { errorMessage, type ProjectObservation } from "@/api";
import { useStableCallback } from "@/ui";
import { useAppServices } from "./useAppServices";

export class ProjectObservationFailure extends Error {
  readonly _tag = "ProjectObservationFailure";
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
  }
}

export function useProjectObservation(
  identity: string | object | null,
  projectID: string,
  consume: (observation: ProjectObservation) => Effect.Effect<void, ProjectObservationFailure>,
) {
  const { api } = useAppServices();
  const handle = useStableCallback(consume);
  const observation = useMemo(
    () =>
      Atom.make(
        (identity === null ? Stream.empty : api.subscribeProject(projectID)).pipe(
          Stream.mapEffect((value) =>
            handle(value).pipe(
              Effect.as(value.kind === "error" ? value.error : null),
              Effect.catch((error) => Effect.succeed(error)),
            ),
          ),
          Stream.scan<Error | null, Error | null>(null, (previous, error) => previous ?? error),
          Stream.prepend([null]),
        ),
        { initialValue: null },
      ),
    [api, handle, identity, projectID],
  );
  const result = useAtomSuspense(observation);
  const retry = useAtomRefresh(observation);
  return { error: result.value, retry };
}
