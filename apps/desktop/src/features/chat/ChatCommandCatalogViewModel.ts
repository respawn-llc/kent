import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { ChatSettingsTarget } from "@/api";
import { queryAtom, type AppServices } from "@/app-facade";
import { composerReadOptions } from "./ComposerDraftViewModel";

export function createChatCommandCatalogViewModel({
  services,
  client,
  target,
}: Readonly<{ services: AppServices; client: QueryClient; target: Atom.Atom<ChatSettingsTarget> }>) {
  const current = Atom.make((get) => {
    const selected = get(target);
    const observer = new QueryObserver(client, {
      ...composerReadOptions,
      queryKey: ["chat-command-catalog", crypto.randomUUID()],
      staleTime: Infinity,
      queryFn: async () => services.api.chat.getCommandCatalog(selected),
    });
    return { observer, read: queryAtom(observer) };
  });
  const read = Atom.make((get) => get(get(current).read));
  const retry = Atom.fn<undefined>()(
    (_, get) => Effect.promise(async () => get(current).observer.refetch()),
    { concurrent: true },
  );
  return { read, retry } as const;
}
