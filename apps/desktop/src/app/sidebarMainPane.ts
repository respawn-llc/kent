export function sidebarProtectedMain(main: Element | null): Element | null {
  return main?.querySelector("[data-sidebar-protected-main]") ?? main;
}
