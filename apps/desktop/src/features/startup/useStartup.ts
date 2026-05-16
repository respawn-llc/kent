import { useQuery } from "@tanstack/react-query";

import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import type { ServerCapabilities, ServerReadiness } from "../../api";
import { StartupConfigurationError, errorMessage } from "../../api/errors";

export type StartupViewModel =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ kind: "ready"; readiness: ServerReadiness; capabilities: ServerCapabilities }>
  | Readonly<{ kind: "error"; titleKey: string; body: string; retry(): void }>;

export function useStartup(): StartupViewModel {
  const { api } = useAppServices();
  const readiness = useQuery({
    queryKey: queryKeys.readiness,
    queryFn: async () => api.getReadiness(),
  });
  const capabilities = useQuery({
    queryKey: queryKeys.capabilities,
    queryFn: async () => api.getCapabilities(),
    enabled: readiness.data?.ready === true,
  });

  if (readiness.isPending) {
    return { kind: "loading" };
  }
  if (readiness.isError) {
    return startupError(readiness.error instanceof StartupConfigurationError ? "startup.errorTitle" : "startup.unreachableTitle", errorMessage(readiness.error), () => {
      void readiness.refetch();
    });
  }
  if (!readiness.data.ready) {
    return startupError("startup.errorTitle", readinessCause(readiness.data), () => {
      void readiness.refetch();
    });
  }
  if (capabilities.isPending) {
    return { kind: "loading" };
  }
  if (capabilities.isError) {
    return startupError("startup.capabilityTitle", errorMessage(capabilities.error), () => {
      void capabilities.refetch();
    });
  }
  const missing = capabilities.data.capabilities.find((capability) => capability.requiredForMvp && !capability.available);
  if (missing !== undefined) {
    return startupError("startup.capabilityTitle", missing.reason, () => {
      void capabilities.refetch();
    });
  }
  return { kind: "ready", readiness: readiness.data, capabilities: capabilities.data };
}

function startupError(titleKey: string, body: string, retry: () => void): StartupViewModel {
  return { kind: "error", titleKey, body: body.length > 0 ? body : "Unknown startup failure", retry };
}

function readinessCause(readiness: ServerReadiness): string {
  const cause = readiness.causes[0];
  if (cause === undefined) {
    return "Readiness check failed.";
  }
  return `${cause.summary} ${cause.nextAction}`.trim();
}
