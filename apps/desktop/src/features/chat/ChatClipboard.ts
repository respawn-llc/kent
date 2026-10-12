import { useMemo } from "react";
import { MutationObserver, useQueryClient } from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Data from "effect/Data";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/api";
import { queryAtom, useAppServices, useStatusController } from "@/app-facade";
import { insertTextAtSelection } from "@/shared/text-input";

class ClipboardPasteError extends Data.TaggedError("ClipboardPasteError")<{
  cause: unknown;
}> {}

export function useChatClipboard() {
  const { nativeBridge } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const read = nativeBridge.clipboard.paste;
  const model = useMemo(() => {
    const observer = new MutationObserver(client, {
      mutationFn: async (input: HTMLTextAreaElement) => {
        if (read === null) return;
        const text = await read();
        if (text !== null) insertTextAtSelection(input, text);
      },
      retry: false,
      networkMode: "always",
      onError: (error) => {
        push({
          id: "chat-clipboard-error",
          tone: "danger",
          title: t("chatComposer.pasteFailed"),
          body: errorMessage(error),
        });
      },
    });
    return {
      state: queryAtom(observer),
      paste: Atom.fn<HTMLTextAreaElement>()(
        (input) =>
          Effect.gen(function* () {
            if (observer.getCurrentResult().isPending || input.readOnly || input.disabled) return;
            yield* Effect.tryPromise({
              try: async () => observer.mutate(input),
              catch: (cause) => new ClipboardPasteError({ cause }),
            }).pipe(Effect.ignore);
          }),
        { concurrent: true },
      ),
    };
  }, [client, read, push, t]);
  const state = useAtomValue(model.state);
  const paste = useAtomSet(model.paste);
  return { supported: read !== null, pending: state.isPending, paste };
}
