import type { QueryClient } from "@tanstack/react-query";
import type * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";
import type { ChatSettingsTarget } from "@/api";
import type { AppServices } from "@/app-facade";
import { createComposerDraftViewModel } from "./ComposerDraftViewModel";
import { createComposerInputViewModel, type ComposerSubmission } from "./ComposerInputViewModel";
import { createComposerPendingViewModel } from "./ComposerPendingViewModel";
import { createComposerHistoryViewModel } from "./ComposerHistoryViewModel";

export function createChatComposerViewModel(
  options: Readonly<{
    services: AppServices;
    client: QueryClient;
    target: Atom.Atom<ChatSettingsTarget>;
    opening: ChatSettingsTarget;
    submission: Atom.Atom<ComposerSubmission>;
    t: TFunction;
  }>,
) {
  const draft = createComposerDraftViewModel(options);
  const input = createComposerInputViewModel({ ...options, draft });
  const pending = createComposerPendingViewModel(options);
  const history = createComposerHistoryViewModel({ ...options, draft, pending: input.pending });
  return { target: options.target, submission: options.submission, draft, input, pending, history } as const;
}
export type ChatComposerViewModel = ReturnType<typeof createChatComposerViewModel>;
