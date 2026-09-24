import type { ReactNode } from "react";
import { useCallback, useMemo } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";

import {
  CurrentWindowChromeTitleContext,
  WindowChromeTitleControllerContext,
  type WindowChromeTitleController,
  type WindowChromeTitleRegistration,
} from "./windowChromeTitle";

export type WindowChromeTitleProviderProps = Readonly<{
  children: ReactNode;
}>;

export function WindowChromeTitleProvider({ children }: WindowChromeTitleProviderProps) {
  const model = useMemo(() => {
    const registrations = Atom.make<readonly WindowChromeTitleRegistration[]>([]);
    return { registrations, title: Atom.make((get) => get(registrations).at(-1)?.title ?? null) };
  }, []);
  const setRegistrations = useAtomSet(model.registrations);
  const title = useAtomValue(model.title);
  const setTitle = useCallback(
    (nextTitle: string | null) => {
      const id = Symbol("window-chrome-title");
      const registration = { id, title: nextTitle };
      setRegistrations((current) => current.concat(registration));
      return () => {
        setRegistrations((current) => current.filter((item) => item.id !== id));
      };
    },
    [setRegistrations],
  );
  const controller = useMemo<WindowChromeTitleController>(
    () => ({
      setTitle,
    }),
    [setTitle],
  );

  return (
    <WindowChromeTitleControllerContext.Provider value={controller}>
      <CurrentWindowChromeTitleContext.Provider value={title}>
        {children}
      </CurrentWindowChromeTitleContext.Provider>
    </WindowChromeTitleControllerContext.Provider>
  );
}
