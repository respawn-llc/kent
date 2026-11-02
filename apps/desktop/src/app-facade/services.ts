import type { NativeBridge, SettingsSnapshot } from "@app/native-bridge";

import type { ApiService } from "@/api";
import type { AppObservationLogger } from "./logging";
import type { SpeechTransfer } from "./speech";

export type AppStorageNamespace = Readonly<{
  kind: "server-address";
  identity: string;
}>;

export type AppServices = Readonly<{
  api: ApiService;
  initialSettings: SettingsSnapshot;
  speech: SpeechTransfer;
  endpoint: string;
  homePath: string;
  logger: AppObservationLogger;
  nativeBridge: NativeBridge;
  protocolVersion: string;
  storageNamespace: AppStorageNamespace | null;
}>;
