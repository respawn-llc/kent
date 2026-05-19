import type { NativeBridge } from "@builder/desktop-native-bridge";

import type { BuilderApiClient } from "../api/client";
import type { GuiLogger } from "./logging";

export type AppServices = Readonly<{
  api: BuilderApiClient;
  debugThemeOverrideEnabled: boolean;
  endpoint: string;
  homePath: string;
  logger: GuiLogger;
  nativeBridge: NativeBridge;
}>;
