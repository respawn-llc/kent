import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import * as Effect from "effect/Effect";

import { errorMessage } from "@/api";
import { queryKeys, reportNonCancelledError, useAppServices, useProjectObservation } from "@/app-facade";
import { ErrorState, useStableCallback } from "@/ui";
import { createProjectLabelEffects } from "./labelEventEffects";
import { ProjectLabelDataContext } from "./projectLabelContext";
import { useManagedProjectLabelFilter } from "./projectLabelFilter";

export function ProjectLabelsProvider({
  children,
  onBackgroundError,
  queryEnabled = true,
  subscribeToProject = true,
  projectID,
}: Readonly<{
  children: ReactNode;
  onBackgroundError?: ((error: unknown) => void) | undefined;
  queryEnabled?: boolean | undefined;
  subscribeToProject?: boolean | undefined;
  projectID: string;
}>) {
  const { api, logger } = useAppServices();
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const catalog = useQuery({
    queryKey: queryKeys.projectLabels(projectID),
    queryFn: async () => api.listProjectLabels(projectID),
    enabled: queryEnabled,
    retry: false,
  });
  const catalogLabelIDs = useMemo(
    () => catalog.data?.labels.map((label) => label.id) ?? null,
    [catalog.data],
  );
  const filter = useManagedProjectLabelFilter(projectID, catalogLabelIDs);
  const reportBackgroundError = useStableCallback((error: unknown) => {
    reportNonCancelledError(error, (failure) => {
      onBackgroundError?.(failure);
      void logger.append("warn", "Project label refresh failed.", {
        error: errorMessage(failure),
        projectID,
      });
    });
  });
  const reportedPersistenceError = useRef<unknown>(null);
  useEffect(() => {
    if (filter.persistence.status !== "error") {
      reportedPersistenceError.current = null;
      return;
    }
    if (reportedPersistenceError.current === filter.persistence.error) {
      return;
    }
    reportedPersistenceError.current = filter.persistence.error;
    reportBackgroundError(filter.persistence.error);
  }, [filter.persistence, reportBackgroundError]);
  const effects = useMemo(
    () =>
      createProjectLabelEffects({
        onFilterAction: filter.dispatch,
        onBackgroundError: reportBackgroundError,
        projectID,
        queryClient,
      }),
    [filter.dispatch, projectID, queryClient, reportBackgroundError],
  );
  const { error, retry } = useProjectObservation(
    subscribeToProject && projectID.length > 0 ? projectID : null,
    projectID,
    (observation) =>
      Effect.promise(async () => {
        switch (observation.kind) {
          case "open":
            await effects.refreshAfterSubscriptionBoundary().catch(reportBackgroundError);
            break;
          case "event":
            await effects.consumeProjectEvent(observation.event).catch(reportBackgroundError);
            break;
          case "complete":
            if (observation.code === 0)
              await effects.refreshAfterSubscriptionBoundary().catch(reportBackgroundError);
            break;
          case "error":
            reportBackgroundError(observation.error);
            break;
        }
      }),
  );
  const value = useMemo(
    () => ({
      catalog,
      effects,
      filter,
      projectID,
    }),
    [catalog, effects, filter, projectID],
  );
  return (
    <ProjectLabelDataContext.Provider value={value}>
      {error === null ? null : (
        <ErrorState
          fullPage={false}
          body={errorMessage(error)}
          title={t("labels.loadFailed")}
          onRetry={retry}
          retryLabel={t("app.retry")}
        />
      )}
      {children}
    </ProjectLabelDataContext.Provider>
  );
}
