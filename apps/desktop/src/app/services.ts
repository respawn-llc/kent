import type { NativeBridge } from "@builder/desktop-native-bridge";

import type { BuilderApiClient } from "../api/client";
import type { GuiLogger } from "./logging";

export type AppServices = Readonly<{
  api: BuilderApiClient;
  endpoint: string;
  logger: GuiLogger;
  nativeBridge: NativeBridge;
}>;
