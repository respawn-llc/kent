import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import * as Scope from "effect/Scope";
import * as Exit from "effect/Exit";
import { useAtomValue } from "@effect/atom-react";
import { useLayoutEffect } from "react";

import type { ChatSessionTarget, ChatTranscriptCommittedRow } from "@/api";
import { queryAtom, queryKeys, type AppServices } from "@/app-facade";

export type ViewedImagePreview = Readonly<{
  kind: "image";
  source: "local" | "server";
  url: string;
  width: number;
  height: number;
}>;
export type ViewedImageOutcome = ViewedImagePreview | Readonly<{ kind: "unavailable" }>;

export type ViewedImagePreviewInput = Readonly<{
  services: AppServices;
  client: QueryClient;
  target: ChatSessionTarget;
  effectiveWorkdir: string | null;
  path: string;
  locator: ChatTranscriptCommittedRow["Locator"];
}>;

type ReadRequest = Readonly<{
  signal: AbortSignal;
  resolve(preview: ViewedImageOutcome): void;
  reject(error: Error): void;
}>;

export function createViewedImagePreviewModel(input: ViewedImagePreviewInput) {
  const owner = Atom.make((get) => {
    const read = Atom.fn((request: ReadRequest) =>
      Effect.scoped(
        Effect.raceFirst(
          Effect.gen(function* () {
            const preview = yield* resolveOriginal(input);
            request.resolve(preview);
            if (preview.kind === "unavailable") return;
            return yield* Effect.never;
          }).pipe(
            Effect.catch((error) =>
              Effect.sync(() => {
                request.reject(error.cause);
              }),
            ),
          ),
          observationEnded(request),
        ),
      ),
    ).pipe(Atom.setIdleTTL(0));
    get.mount(read);
    const observer = new QueryObserver(input.client, {
      queryKey: queryKeys.viewedImage(input.target.sessionID, input.locator),
      queryFn: async ({ signal }) =>
        new Promise<ViewedImageOutcome>((resolve, reject) => {
          get.set(read, { signal, resolve, reject });
        }),
      retry: false,
      networkMode: "always",
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      gcTime: 0,
    });
    return { observer, client: input.client, result: queryAtom(observer).pipe(Atom.setIdleTTL(0)) };
  }).pipe(Atom.setIdleTTL(0));
  return owner;
}

export function useViewedImagePreview(model: ReturnType<typeof createViewedImagePreviewModel>) {
  const owner = useAtomValue(model);
  // Atom finalizers are low-priority; end Query observation at the React boundary.
  useLayoutEffect(() => {
    // Strict mount replay reattaches the same observer after cleanup removes its Query.
    owner.observer.setOptions(owner.observer.options);
    return () => {
      const filter = { queryKey: owner.observer.options.queryKey, exact: true };
      void owner.client.cancelQueries(filter);
      owner.client.removeQueries(filter);
    };
  }, [owner]);
  return useAtomValue(owner.result);
}

const resolveOriginal = Effect.fnUntraced(function* (input: ViewedImagePreviewInput) {
  const localRead = input.services.nativeBridge.files.readImage;
  const effectiveWorkdir = input.effectiveWorkdir;
  if (localRead !== null) {
    const bytes = yield* Effect.tryPromise({
      try: async () => localRead({ path: input.path, basePath: effectiveWorkdir ?? undefined }),
      catch: asError,
    }).pipe(
      Effect.catch((failure) =>
        Effect.promise(async () => {
          await input.services.logger.append("warn", "Viewed image local source unavailable.", {
            path: input.path,
            error: failure.cause.message,
          });
          return null;
        }),
      ),
    );
    if (bytes !== null) {
      const image = yield* decodeOriginal(input, bytes, "local");
      if (image !== null) return image;
    }
  }
  const server = yield* Effect.tryPromise({
    try: async () => input.services.api.chat.getViewedImageOriginal(input.target, input.path),
    catch: asError,
  });
  if (server.kind === "content") {
    const image = yield* decodeOriginal(input, server.bytes, "server");
    if (image !== null) return image;
  }
  return { kind: "unavailable" } as const;
});

const decodeOriginal = Effect.fnUntraced(function* (
  input: ViewedImagePreviewInput,
  bytes: Uint8Array,
  source: ViewedImagePreview["source"],
) {
  const parent = yield* Effect.scope;
  const candidate = yield* Scope.fork(parent);
  const url = yield* Effect.acquireRelease(
    Effect.sync(() => URL.createObjectURL(new Blob([new Uint8Array(bytes)]))),
    (url) =>
      Effect.sync(() => {
        URL.revokeObjectURL(url);
      }),
  ).pipe(Effect.provideService(Scope.Scope, candidate));
  const image = new Image();
  image.src = url;
  const decoded = yield* Effect.tryPromise({
    try: async () => image.decode(),
    catch: asError,
  }).pipe(
    Effect.as(true),
    Effect.catch((failure) =>
      Effect.promise(async () => {
        await input.services.logger.append("warn", "Viewed image original could not decode.", {
          path: input.path,
          source,
          error: failure.cause.message,
        });
        return false;
      }),
    ),
  );
  if (!decoded) {
    yield* Scope.close(candidate, Exit.void);
    return null;
  }
  return {
    kind: "image",
    source,
    url,
    width: image.naturalWidth,
    height: image.naturalHeight,
  } as const;
});

class ImageSourceFailure {
  readonly _tag = "ImageSourceFailure";
  constructor(readonly cause: Error) {}
}

function asError(cause: unknown): ImageSourceFailure {
  return new ImageSourceFailure(cause instanceof Error ? cause : new Error(String(cause)));
}

const observationEnded = Effect.fnUntraced(function* (request: ReadRequest) {
  return yield* Effect.callback<never>((resume) => {
    const abort = () => {
      request.reject(new DOMException("Image observation ended.", "AbortError"));
      resume(Effect.interrupt);
    };
    if (request.signal.aborted) {
      abort();
      return;
    }
    request.signal.addEventListener("abort", abort, { once: true });
    return Effect.sync(() => {
      request.signal.removeEventListener("abort", abort);
    });
  });
});
