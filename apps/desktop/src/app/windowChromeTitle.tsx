/* eslint-disable react-refresh/only-export-components -- Title hooks share provider context in one small module. */
import type { ReactNode } from "react";
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

type WindowChromeTitleRegistration = Readonly<{
  id: symbol;
  title: string | null;
}>;

export type WindowChromeTitleController = Readonly<{
  setTitle(title: string | null): () => void;
}>;

const WindowChromeTitleControllerContext = createContext<WindowChromeTitleController | null>(null);
const CurrentWindowChromeTitleContext = createContext<string | null>(null);

export type WindowChromeTitleProviderProps = Readonly<{
  children: ReactNode;
}>;

export function WindowChromeTitleProvider({ children }: WindowChromeTitleProviderProps) {
  const [registrations, setRegistrations] = useState<readonly WindowChromeTitleRegistration[]>([]);
  const title = registrations[registrations.length - 1]?.title ?? null;
  const setTitle = useCallback((nextTitle: string | null) => {
    const id = Symbol("window-chrome-title");
    const registration = { id, title: nextTitle };
    setRegistrations((current) => current.concat(registration));
    return () => {
      setRegistrations((current) => current.filter((item) => item.id !== id));
    };
  }, []);
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

export function useWindowChromeTitle(title: string | null, enabled = true): void {
  const controller = useContext(WindowChromeTitleControllerContext);
  const normalizedTitle = normalizeWindowChromeTitle(title);

  useEffect(() => {
    if (controller === null || !enabled) {
      return undefined;
    }
    return controller.setTitle(normalizedTitle);
  }, [controller, enabled, normalizedTitle]);
}

export function useCurrentWindowChromeTitle(): string | null {
  return useContext(CurrentWindowChromeTitleContext);
}

function normalizeWindowChromeTitle(title: string | null | undefined): string | null {
  const trimmed = title?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : null;
}
