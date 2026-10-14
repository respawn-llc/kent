import type { ReactNode } from "react";
import { MarkdownLinkContext } from "./MarkdownLinkContext";

export function MarkdownLinkProvider({
  children,
  openExternal,
}: Readonly<{ children: ReactNode; openExternal(url: string): void }>) {
  return <MarkdownLinkContext.Provider value={openExternal}>{children}</MarkdownLinkContext.Provider>;
}
