import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useMemo } from "react";
import { I18nextProvider } from "react-i18next";

import { appI18n, initializeI18n } from "../i18n/setup";
import { createAppQueryClient } from "./queryClient";
import type { AppServices } from "./services";
import { AppServicesProvider } from "./servicesContext";
import { StatusProvider } from "./statusStore";

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
          <StatusProvider>{children}</StatusProvider>
        </AppServicesProvider>
      </QueryClientProvider>
    </I18nextProvider>
  );
}
