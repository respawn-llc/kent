import { expect, it, vi } from "bun:test";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import type { ReactNode } from "react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { catalog, newChatTarget } from "@/test-support/chat-destination";
import { appI18n } from "@/i18n";
import { createChatSettingsViewModel } from "./ChatSettingsViewModel";

it("starts a shortcut Chat with the requested Agent's server baseline", async () => {
  const services = createTestServices([]);
  const defaultChoice = catalog.catalog.choices[0];
  if (defaultChoice === undefined) throw new Error("Missing default Agent fixture");
  const baseline = {
    ...defaultChoice.baseline,
    agentRole: "reviewer",
    supervisor: "off" as const,
    questionsEnabled: false,
  };
  vi.spyOn(services.api.chat, "getSettings").mockResolvedValue({
    ...catalog,
    catalog: {
      ...catalog.catalog,
      choices: [
        defaultChoice,
        { ...defaultChoice, agent: { ...defaultChoice.agent, role: "reviewer" }, baseline },
      ],
    },
  });
  const client = new QueryClient();
  const model = createChatSettingsViewModel({
    services,
    client,
    t: appI18n.t,
    target: Atom.make(newChatTarget),
    initialAgentRole: "reviewer",
  });
  const { result } = renderHook(() => useAtomValue(model.state), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services} queryClient={client}>
        {children}
      </TestAppProviders>
    ),
  });
  await waitFor(() => {
    expect(result.current).toMatchObject({ kind: "ready-new-chat", initialSettings: baseline });
  });
});

it("surfaces an unavailable requested Agent as a failed read instead of selecting another Agent", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getSettings").mockResolvedValue(catalog);
  const client = new QueryClient();
  const model = createChatSettingsViewModel({
    services,
    client,
    t: appI18n.t,
    target: Atom.make(newChatTarget),
    initialAgentRole: "missing-agent",
  });
  const { result } = renderHook(() => useAtomValue(model.state), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services} queryClient={client}>
        {children}
      </TestAppProviders>
    ),
  });
  await waitFor(() => {
    expect(result.current.kind).toBe("failed-new-chat");
  });
});
