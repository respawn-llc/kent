import { SidebarRootOwner } from "@/app-facade";
import { type ReactNode } from "react";

import { SidebarHost } from "./sidebar";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { SidebarProvider } from "./sidebarProvider";

export function SidebarComposition({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <SidebarProvider policy={sidebarDestinationPolicy}>
      <SidebarRootOwner>{children}</SidebarRootOwner>
      <SidebarHost />
    </SidebarProvider>
  );
}
