import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import { createElement, useState, type ReactNode } from "react";

import { AppChrome } from "@/app";
import { SidebarRootOwner } from "@/app-facade";

export function TestSidebar({ children }: Readonly<{ children: ReactNode }>) {
  const [router] = useState(() =>
    createRouter({
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: createRootRoute(),
    }),
  );
  return createElement(RouterContextProvider, {
    router,
    children: createElement(AppChrome, {
      children: createElement(SidebarRootOwner, { children }),
    }),
  });
}
