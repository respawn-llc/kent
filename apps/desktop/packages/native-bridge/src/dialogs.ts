import { LogicalPosition, LogicalSize } from "@tauri-apps/api/dpi";
import { WebviewWindow } from "@tauri-apps/api/webviewWindow";
import { Effect, EffectState, getCurrentWindow } from "@tauri-apps/api/window";

export type NativeDialogWindowOptions = Readonly<{
  label: string;
  title: string;
  route: string;
  params: Readonly<Record<string, string>>;
  theme?: NativeDialogTheme;
  initialWidth?: number;
  initialHeight?: number;
  maximizable?: boolean;
  resizable?: boolean;
}>;

export type NativeDialogTheme = "light" | "dark";

export type NativeDialogContentSize = Readonly<{
  width: number;
  height: number;
}>;

const nativeDialogThemeSearchParam = "__builderTheme";
const builderThemeAttribute = "data-builder-theme";

export async function openNativeDialogWindow(options: NativeDialogWindowOptions): Promise<void> {
  const url = routeWithParams(options.route, withDialogTheme(options.params, options.theme ?? readEffectiveParentTheme()));
  const label = options.label.startsWith("native-dialog-") ? options.label : `native-dialog-${options.label}`;
  const placement = await centeredOnCurrentWindow(options);
  await new Promise<void>((resolve, reject) => {
    const window = new WebviewWindow(label, {
      alwaysOnTop: true,
      center: false,
      closable: true,
      decorations: true,
      focus: false,
      height: placement.height,
      hiddenTitle: true,
      maximizable: options.maximizable ?? false,
      parent: getCurrentWindow(),
      preventOverflow: true,
      resizable: options.resizable ?? false,
      shadow: true,
      skipTaskbar: true,
      title: options.title,
      titleBarStyle: "overlay",
      trafficLightPosition: new LogicalPosition(20, 18),
      transparent: true,
      url,
      visible: false,
      width: placement.width,
      windowEffects: {
        effects: [Effect.UnderWindowBackground, Effect.Acrylic],
        radius: 18,
        state: EffectState.Active,
      },
      x: placement.x,
      y: placement.y,
    });
    window
      .once("tauri://created", () => {
        bringDialogToFront(window).then(resolve, reject);
      })
      .catch(reject);
    window
      .once("tauri://error", (event) => {
        reject(new Error(String(event.payload)));
      })
      .catch(reject);
  });
}

async function centeredOnCurrentWindow(options: NativeDialogWindowOptions): Promise<
  Readonly<{
    height: number;
    width: number;
    x: number;
    y: number;
  }>
> {
  const parent = getCurrentWindow();
  const scaleFactor = await parent.scaleFactor();
  const parentPosition = await parent.outerPosition();
  const parentSize = await parent.outerSize();
  const width = options.initialWidth ?? 520;
  const height = options.initialHeight ?? 360;
  const parentX = parentPosition.x / scaleFactor;
  const parentY = parentPosition.y / scaleFactor;
  const parentWidth = parentSize.width / scaleFactor;
  const parentHeight = parentSize.height / scaleFactor;
  return {
    height,
    width,
    x: Math.round(parentX + (parentWidth - width) / 2),
    y: Math.round(parentY + (parentHeight - height) / 2),
  };
}

async function bringDialogToFront(window: WebviewWindow): Promise<void> {
  await window.show();
  await window.setAlwaysOnTop(true);
  await window.setFocus();
}

export async function fitCurrentWindowToContent(size: NativeDialogContentSize): Promise<void> {
  const logicalSize = new LogicalSize(
    Math.max(1, Math.ceil(size.width)),
    Math.max(1, Math.ceil(size.height)),
  );
  const window = getCurrentWindow();
  await window.setMinSize(null);
  await window.setMaxSize(null);
  await window.setSize(logicalSize);
  await window.setMinSize(logicalSize);
  await window.setMaxSize(logicalSize);
}

function routeWithParams(route: string, params: Readonly<Record<string, string>>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    search.set(key, value);
  }
  const suffix = search.size > 0 ? `?${search.toString()}` : "";
  return `${route}${suffix}`;
}

function withDialogTheme(
  params: Readonly<Record<string, string>>,
  theme: NativeDialogTheme,
): Readonly<Record<string, string>> {
  if (params[nativeDialogThemeSearchParam] !== undefined) {
    return params;
  }
  return {
    ...params,
    [nativeDialogThemeSearchParam]: theme,
  };
}

function readEffectiveParentTheme(): NativeDialogTheme {
  const configured = document.documentElement.getAttribute(builderThemeAttribute);
  if (configured === "light" || configured === "dark") {
    return configured;
  }
  if (typeof window.matchMedia !== "function") {
    return "dark";
  }
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}
