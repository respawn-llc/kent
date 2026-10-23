import { createContext } from "react";

export type MarkdownLinkActions = Readonly<{
  openExternal(url: string): void;
  openFile?: ((path: string) => void) | undefined;
}>;

export const MarkdownLinkContext = createContext<MarkdownLinkActions | null>(null);
