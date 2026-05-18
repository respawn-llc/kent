import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useMemo } from "react";
import { I18nextProvider } from "react-i18next";

import { appI18n, initializeI18n } from "../i18n/setup";
import { useReconnectRefresh } from "./connectionRefresh";
import { createAppQueryClient } from "./queryClient";
import type { AppServices } from "./services";
import { AppServicesProvider } from "./servicesContext";
import { StatusProvider } from "./statusStore";
import { useTaskDetailMutationInvalidator } from "./taskDetailInvalidation";
import { WindowChromeTitleProvider } from "./windowChromeTitle";

void initializeI18n();

export type AppProvidersProps = Readonly<{
  services: AppServices;
  children: ReactNode;
}>;

export function AppProviders({ services, children }: AppProvidersProps) {
  const queryClient = useMemo(() => createAppQueryClient(), []);

  return (
    <I18nextProvider i18n={appI18n}>
      <QueryClientProvider client={queryClient}>
        <AppServicesProvider services={services}>
          <WindowChromeTitleProvider>
            <StatusProvider>
              <ReconnectRefresh />
              <TaskDetailMutationInvalidator />
              {children}
            </StatusProvider>
          </WindowChromeTitleProvider>
        </AppServicesProvider>
      </QueryClientProvider>
    </I18nextProvider>
  );
}

function TaskDetailMutationInvalidator() {
  useTaskDetailMutationInvalidator();
  return null;
}

function ReconnectRefresh() {
  useReconnectRefresh();
  return null;
}
