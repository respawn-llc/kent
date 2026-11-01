import type { ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";
import { TestAppProviders as AppProviders, type TestAppServices } from "../app-services";
import { SpeechDestinationProvider } from "@/shared/speech-input";

export function TestAppProviders({
  children,
  services,
}: Readonly<{ children: ReactNode; services: TestAppServices }>) {
  return (
    <RegistryProvider>
      <AppProviders services={services}>
        <SpeechDestinationProvider>{children}</SpeechDestinationProvider>
      </AppProviders>
    </RegistryProvider>
  );
}

export function composerWrapper(services: TestAppServices) {
  return function Wrapper({ children }: Readonly<{ children: ReactNode }>) {
    return <TestAppProviders services={services}>{children}</TestAppProviders>;
  };
}
