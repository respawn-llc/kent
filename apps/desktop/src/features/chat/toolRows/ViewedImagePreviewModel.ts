import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useLayoutEffect } from "react";

import type { ChatSessionTarget, ChatTranscriptCommittedRow } from "@/api";
import { queryAtom, queryKeys, type AppServices } from "@/app-facade";

type ViewedImagePreviewState =
  | Readonly<{ kind: "resolving" }>
  | Readonly<{ kind: "loading"; url: string }>
  | Readonly<{ kind: "loaded"; url: string; width: number; height: number }>
  | Readonly<{ kind: "source-error"; error: Error }>
  | Readonly<{ kind: "image-error" }>;

type ImageLoadState =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "loaded"; width: number; height: number }>
  | Readonly<{ kind: "error" }>;

type ImageDimensions = Readonly<{ width: number; height: number }>;

export type ViewedImagePreviewInput = Readonly<{
  services: AppServices;
  client: QueryClient;
  target: ChatSessionTarget;
  path: string;
  locator: ChatTranscriptCommittedRow["Locator"];
}>;

export function createViewedImagePreviewModel(input: ViewedImagePreviewInput) {
  const queryOptions = {
    queryKey: queryKeys.viewedImage(input.target.sessionID, input.locator),
    queryFn: async ({ signal }) => {
      const source = await input.services.api.chat.resolveViewedImageOriginalSource(
        input.target,
        input.path,
        signal,
      );
      return resolveViewedImageURL(input.services.endpoint, source);
    },
    retry: false,
    networkMode: "always" as const,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    gcTime: 0,
  };
  const observer = new QueryObserver(input.client, queryOptions);
  const source = queryAtom(observer);
  const active = Atom.make(true);
  const imageLoad = Atom.make<ImageLoadState>({ kind: "loading" });
  const loaded = Atom.fn<ImageDimensions>()((dimensions, get) =>
    Effect.sync(() => {
      if (!get(active)) return;
      if (dimensions.width <= 0 || dimensions.height <= 0) {
        get.set(imageLoad, { kind: "error" });
        return;
      }
      get.set(imageLoad, { kind: "loaded", ...dimensions });
    }),
  );
  const failed = Atom.fn<undefined>()((_input, get) =>
    Effect.gen(function* () {
      if (!get(active)) return;
      get.set(imageLoad, { kind: "error" });
      yield* Effect.promise(async () =>
        input.services.logger.append("warn", "Viewed image preview failed to load.", {
          session_id: input.target.sessionID,
          path: input.path,
        }),
      );
    }),
  );
  const state = Atom.make((get): ViewedImagePreviewState => {
    const request = get(source);
    if (request.isError) return { kind: "source-error", error: request.error };
    if (request.data === undefined) return { kind: "resolving" };
    const image = get(imageLoad);
    if (image.kind === "error") return { kind: "image-error" };
    if (image.kind === "loading") return { kind: "loading", url: request.data };
    return { kind: "loaded", url: request.data, width: image.width, height: image.height };
  });
  return { active, client: input.client, failed, loaded, observer, queryOptions, state } as const;
}

function resolveViewedImageURL(endpoint: string, source: string): string {
  const imageEndpoint = new URL(endpoint);
  switch (imageEndpoint.protocol) {
    case "ws:":
      imageEndpoint.protocol = "http:";
      break;
    case "wss:":
      imageEndpoint.protocol = "https:";
      break;
    default:
      throw new Error("Kent server endpoint must use ws:// or wss://.");
  }
  const imageURL = new URL(source, imageEndpoint);
  if (imageURL.origin !== imageEndpoint.origin) {
    throw new Error("Viewed image source must belong to the configured Kent server.");
  }
  return imageURL.href;
}

export function useViewedImagePreview(model: ReturnType<typeof createViewedImagePreviewModel>) {
  const state = useAtomValue(model.state);
  const setActive = useAtomSet(model.active);
  const loaded = useAtomSet(model.loaded);
  const failed = useAtomSet(model.failed, { mode: "promise" });
  useLayoutEffect(() => {
    setActive(true);
    // Strict mount replay reattaches the same observer after cleanup removes its Query.
    model.observer.setOptions(model.queryOptions);
    return () => {
      setActive(false);
      const filter = { queryKey: model.queryOptions.queryKey, exact: true };
      void model.client.cancelQueries(filter);
      model.client.removeQueries(filter);
    };
  }, [model, setActive]);
  return { state, loaded, failed } as const;
}
