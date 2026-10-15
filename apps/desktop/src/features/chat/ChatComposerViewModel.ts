import type { QueryClient } from "@tanstack/react-query";
import type * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";
import type { ChatSettingsTarget } from "@/api";
import type { AppServices } from "@/app-facade";
import { createComposerDraftViewModel } from "./ComposerDraftViewModel";
import { createComposerInputViewModel, type ComposerSubmission } from "./ComposerInputViewModel";
import { createComposerPendingViewModel } from "./ComposerPendingViewModel";
import { createComposerHistoryViewModel } from "./ComposerHistoryViewModel";
import { createOutgoingInputPresentation } from "./OutgoingInputPresentation";

export function createChatComposerViewModel(
  options: Readonly<{
    services: AppServices;
    client: QueryClient;
    target: Atom.Atom<ChatSettingsTarget | null>;
    opening: Parameters<typeof createComposerDraftViewModel>[0]["opening"];
    submission: Atom.Atom<ComposerSubmission>;
    t: TFunction;
    transferPending?: Atom.Atom<boolean>;
  }>,
) {
  const draft = createComposerDraftViewModel(options);
  const outgoing = createOutgoingInputPresentation(draft);
  const input = createComposerInputViewModel({ ...options, draft, outgoing });
  const pending = createComposerPendingViewModel({
    ...options,
    interactionRestricted: draft.interactionRestricted,
  });
  const history = createComposerHistoryViewModel({ ...options, draft, pending: outgoing.heldReadonly });
  return {
    target: options.target,
    submission: options.submission,
    draft,
    input,
    pending,
    history,
    outgoing,
  } as const;
}
export type ChatComposerViewModel = ReturnType<typeof createChatComposerViewModel>;
