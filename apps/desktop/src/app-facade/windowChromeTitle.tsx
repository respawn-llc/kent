import { createContext, useContext, useEffect, type ReactElement } from "react";

export type WindowChromeTitleRegistration = Readonly<{
  id: symbol;
  title: string | null;
  action: ReactElement | null;
}>;

export type WindowChromeTitleController = Readonly<{
  setTitle(title: string | null, action?: ReactElement | null): () => void;
}>;

export const WindowChromeTitleControllerContext = createContext<WindowChromeTitleController | null>(null);
export const CurrentWindowChromeTitleContext = createContext<string | null>(null);
export const CurrentWindowChromeActionContext = createContext<ReactElement | null>(null);

export function useWindowChromeTitle(
  title: string | null,
  enabled = true,
  action: ReactElement | null = null,
): void {
  const controller = useContext(WindowChromeTitleControllerContext);
  const normalizedTitle = normalizeWindowChromeTitle(title);

  useEffect(() => {
    if (controller === null || !enabled) {
      return undefined;
    }
    return controller.setTitle(normalizedTitle, action);
  }, [controller, enabled, normalizedTitle, action]);
}

export function useCurrentWindowChromeTitle(): string | null {
  return useContext(CurrentWindowChromeTitleContext);
}

export function useCurrentWindowChromeAction(): ReactElement | null {
  return useContext(CurrentWindowChromeActionContext);
}

function normalizeWindowChromeTitle(title: string | null | undefined): string | null {
  const trimmed = title?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : null;
}
