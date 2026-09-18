import type { ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";
import { TestAppProviders as AppProviders, type TestAppServices } from "../app-services";

export function TestAppProviders({
  children,
  services,
}: Readonly<{ children: ReactNode; services: TestAppServices }>) {
  return (
    <RegistryProvider>
      <AppProviders services={services}>{children}</AppProviders>
    </RegistryProvider>
  );
}

export function composerWrapper(services: TestAppServices) {
  return function Wrapper({ children }: Readonly<{ children: ReactNode }>) {
    return <TestAppProviders services={services}>{children}</TestAppProviders>;
  };
}
