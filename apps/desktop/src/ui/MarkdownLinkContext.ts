import { createContext } from "react";

export const MarkdownLinkContext = createContext<((url: string) => void) | null>(null);
