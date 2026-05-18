import { RouterProvider } from "@tanstack/react-router";
import { useMemo } from "react";

import { applyNativeDialogThemeOverride } from "../appEnvironment";
import { AppProviders } from "./AppProviders";
import { createAppRouter } from "./routes";
import type { AppServices } from "./services";

export type AppRootProps = Readonly<{
  services: AppServices;
}>;

export function AppRoot({ services }: AppRootProps) {
  applyNativeDialogThemeOverride();
  const router = useMemo(() => createAppRouter(), []);

  return (
    <AppProviders services={services}>
      <RouterProvider router={router} />
    </AppProviders>
  );
}
