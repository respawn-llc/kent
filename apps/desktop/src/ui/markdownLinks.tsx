import { useMemo, type ReactNode } from "react";
import { MarkdownLinkContext, type MarkdownLinkActions } from "./MarkdownLinkContext";

export function MarkdownLinkProvider({
  children,
  openExternal,
  openFile,
}: MarkdownLinkActions & Readonly<{ children: ReactNode }>) {
  const actions = useMemo(() => ({ openExternal, openFile }), [openExternal, openFile]);
  return <MarkdownLinkContext.Provider value={actions}>{children}</MarkdownLinkContext.Provider>;
}
