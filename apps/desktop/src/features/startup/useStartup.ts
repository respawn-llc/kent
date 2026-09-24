import { useState } from "react";
import {
  QueryObserver,
  useQueryClient,
  type QueryClient,
  type QueryObserverResult,
} from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import { queryAtom, queryKeys, type AppServices, type QuerySnapshot } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import type { ServerReadiness } from "@/api";
import { ProtocolMismatchError, TransportError, errorMessage } from "@/api";

type StartupState =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "ready"; readiness: ServerReadiness }>
  | Readonly<{ kind: "server-missing"; detail: string }>
  | Readonly<{ kind: "error"; titleKey: string; body: string }>;

export type StartupViewModel = ReturnType<typeof useStartup>;

export function useStartup() {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const [model] = useState(() => createStartupModel(services, client, t));
  const state = useAtomValue(model.state);
  const retry = useAtomSet(model.retry);
  return state.kind === "error" || state.kind === "server-missing" ? { ...state, retry } : state;
}

function createStartupModel(services: AppServices, client: QueryClient, t: TFunction) {
  const { api, protocolVersion } = services;
  const observer = new QueryObserver(client, {
    queryKey: queryKeys.readiness,
    queryFn: async () => api.getReadiness(),
  });
  const request = queryAtom(observer);
  const retry = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  const state = Atom.make((get) => projectStartup(get(request), protocolVersion, t));
  return { state, retry } as const;
}

function projectStartup(
  readiness: QuerySnapshot<QueryObserverResult<ServerReadiness>>,
  protocolVersion: AppServices["protocolVersion"],
  t: TFunction,
): StartupState {
  if (readiness.isPending) {
    return { kind: "loading" };
  }
  if (readiness.isError) {
    if (readiness.error instanceof ProtocolMismatchError) {
      return startupError("startup.updateTitle", errorMessage(readiness.error), t("startup.unknownFailure"));
    }
    if (readiness.error instanceof TransportError) {
      // The loopback Kent server is unreachable — the desktop is a thin client and the
      // server is a separate install, so guide first-run setup instead of erroring.
      return {
        kind: "server-missing",
        detail: errorMessage(readiness.error),
      };
    }
    // The server is reachable but the readiness handshake failed (bad config, a
    // persistence-root mismatch, or an RPC/contract error). Surface it as a startup
    // error with retry rather than the first-run setup guide, which would be misleading
    // when the fix is to start or connect to the correct server.
    return startupError("startup.errorTitle", errorMessage(readiness.error), t("startup.unknownFailure"));
  }
  if (!readiness.data.ready) {
    return startupError(
      "startup.errorTitle",
      readinessCause(readiness.data, t("startup.readinessFailed")),
      t("startup.unknownFailure"),
    );
  }
  if (readiness.data.protocolVersion !== protocolVersion) {
    return startupError(
      "startup.updateTitle",
      protocolMismatchBody(
        t("startup.clientProtocol", { protocol: protocolVersion }),
        t("startup.serverProtocol", { protocol: readiness.data.protocolVersion }),
        t("startup.updateSameBuild"),
      ),
      t("startup.unknownFailure"),
    );
  }
  return { kind: "ready", readiness: readiness.data };
}

function startupError(titleKey: string, body: string, fallbackBody: string): StartupState {
  return { kind: "error", titleKey, body: body.length > 0 ? body : fallbackBody };
}

function readinessCause(readiness: ServerReadiness, fallbackBody: string): string {
  const cause = readiness.causes[0];
  if (cause === undefined) {
    return fallbackBody;
  }
  const body = [cause.summary, cause.nextAction]
    .filter((part): part is string => part !== undefined && part.length > 0)
    .join(" ");
  return body.length > 0 ? body : fallbackBody;
}

function protocolMismatchBody(
  clientProtocol: string,
  serverProtocol: string,
  updateInstruction: string,
): string {
  return [clientProtocol, serverProtocol, updateInstruction].join(" ");
}
