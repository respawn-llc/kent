import type { NativeBridge, SettingsSnapshot } from "@app/native-bridge";

import type { ApiService } from "@/api";
import type { AppObservationLogger } from "./logging";

export type AppStorageNamespace = Readonly<{
  kind: "server-address";
  identity: string;
}>;

export type AppServices = Readonly<{
  api: ApiService;
  initialSettings: SettingsSnapshot;
  endpoint: string;
  homePath: string;
  logger: AppObservationLogger;
  nativeBridge: NativeBridge;
  protocolVersion: string;
  storageNamespace: AppStorageNamespace | null;
}>;
