import type { NativeBridge } from "@app/native-bridge";

import type { ApiClient } from "../api/client";
import type { GuiLogger } from "./logging";

export type AppServices = Readonly<{
  api: ApiClient;
  debugThemeOverrideEnabled: boolean;
  endpoint: string;
  homePath: string;
  logger: GuiLogger;
  nativeBridge: NativeBridge;
}>;
