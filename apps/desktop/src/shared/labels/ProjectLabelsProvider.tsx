import { useQueryClient } from "@tanstack/react-query";
import { useAtomMount } from "@effect/atom-react";
import { useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import * as Effect from "effect/Effect";

import { errorMessage } from "@/api";
import { reportNonCancelledError, useAppServices, useProjectObservation } from "@/app-facade";
import { ErrorState, useStableCallback } from "@/ui";
import { ProjectLabelDataContext } from "./projectLabelContext";
import { createProjectLabelsModel } from "./ProjectLabelsModel";

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
  const services = useAppServices();
  const { logger } = services;
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const reportBackgroundError = useStableCallback((error: unknown) => {
    reportNonCancelledError(error, (failure) => {
      onBackgroundError?.(failure);
      void logger.append("warn", "Project label refresh failed.", {
        error: errorMessage(failure),
        projectID,
      });
    });
  });
  const model = useMemo(
    () =>
      createProjectLabelsModel({
        services,
        client: queryClient,
        projectID,
        enabled: queryEnabled,
        report: reportBackgroundError,
      }),
    [services, queryClient, projectID, queryEnabled, reportBackgroundError],
  );
  useAtomMount(model.catalog);
  useAtomMount(model.filter.state);
  const { effects } = model;
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
  return (
    <ProjectLabelDataContext.Provider value={model}>
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
