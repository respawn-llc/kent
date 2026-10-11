import { create } from "@app/server-api-contract";
import {
  AutoCompactionPolicy,
  ChatSettingsService,
  Editability,
  SettingsSnapshotSchema,
  SupervisorValue,
} from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import type { FakeRpcTransport } from "./index";

export function sessionSettingsSubscriptionRoute() {
  const method = ChatSettingsService.method.subscribe;
  return {
    subscriptionDescriptor: method,
    startResult: create(method.output, { outcome: { case: "success", value: {} } }),
  };
}

export function hasSessionSettingsSubscription(transport: FakeRpcTransport): boolean {
  return transport.descriptorSubscriptions.includes(ChatSettingsService.method.subscribe);
}

export function emitSessionSettingsSnapshot(
  transport: FakeRpcTransport,
  sessionID: string,
  name: string | null,
  questionsEnabled: boolean,
): void {
  transport.emitDescriptor(
    ChatSettingsService.method.subscribe,
    ChatSettingsService.method.event,
    create(SettingsSnapshotSchema, {
      sessionName: name ?? undefined,
      session: { sessionId: sessionID },
      settings: {
        selectedAgent: { role: "default", model: "local", thinking: "none" },
        agentChoices: [{ role: "default", model: "local", thinking: "none" }],
        agentEditability: Editability.EDITABLE,
        supervisor: {
          value: SupervisorValue.AFTER_EDITS,
          baseline: SupervisorValue.AFTER_EDITS,
          editability: Editability.EDITABLE,
        },
        questions: { capable: true, enabled: questionsEnabled, editability: Editability.EDITABLE },
        autoCompaction: {
          policy: AutoCompactionPolicy.OPTIONAL,
          stored: true,
          effective: true,
          editability: Editability.EDITABLE,
        },
      },
    }),
  );
}
