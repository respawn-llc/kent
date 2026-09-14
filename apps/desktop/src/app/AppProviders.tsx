import { QueryClientProvider } from "@tanstack/react-query";
import { RegistryProvider } from "@effect/atom-react";
import type { ReactNode } from "react";
import { useMemo } from "react";
import { I18nextProvider } from "react-i18next";

import { appI18n, initializeI18n } from "@/i18n";
import { useReconnectRefresh } from "./connectionRefresh";
import { useWindowFileDrops } from "./fileDrops";
import { useNativeWindowGlassTintSync } from "./nativeWindowGlassTint";
import { createAppQueryClient } from "./queryClient";
import type { AppServices } from "@/app-facade";
import { AppServicesProvider } from "@/app-facade";
import { StatusProvider } from "@/app-facade";
import { TaskSearchMemoryProvider } from "@/app-facade";
import { WindowFocusProvider } from "@/app-facade";
import { WindowChromeTitleProvider } from "@/app-facade";
import { ChatPromptPresenceProvider } from "@/app-facade";

void initializeI18n();

export type AppProvidersProps = Readonly<{
  services: AppServices;
  children: ReactNode;
}>;

export function AppProviders({ services, children }: AppProvidersProps) {
  const queryClient = useMemo(() => createAppQueryClient(), []);
  useWindowFileDrops(services);

  return (
    <I18nextProvider i18n={appI18n}>
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>
          <AppServicesProvider services={services}>
            <WindowFocusProvider>
              <WindowChromeTitleProvider>
                <StatusProvider>
                  <TaskSearchMemoryProvider>
                    <ChatPromptPresenceProvider>
                      <ReconnectRefresh />
                      <NativeWindowGlassTintSync nativeBridge={services.nativeBridge} />
                      {children}
                    </ChatPromptPresenceProvider>
                  </TaskSearchMemoryProvider>
                </StatusProvider>
              </WindowChromeTitleProvider>
            </WindowFocusProvider>
          </AppServicesProvider>
        </RegistryProvider>
      </QueryClientProvider>
    </I18nextProvider>
  );
}

function NativeWindowGlassTintSync({
  nativeBridge,
}: Readonly<{ nativeBridge: AppServices["nativeBridge"] }>) {
  useNativeWindowGlassTintSync(nativeBridge);
  return null;
}

function ReconnectRefresh() {
  useReconnectRefresh();
  return null;
}
