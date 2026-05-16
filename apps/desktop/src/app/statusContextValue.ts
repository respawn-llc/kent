import { createContext } from "react";

import type { StatusNotice } from "../ui";

export type StatusController = Readonly<{
  notices: readonly StatusNotice[];
  push(notice: StatusNotice): void;
  dismiss(id: string): void;
}>;

export const StatusContext = createContext<StatusController | null>(null);
