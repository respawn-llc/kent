import { invoke } from "@tauri-apps/api/core";
import { LogicalPosition, LogicalSize } from "@tauri-apps/api/dpi";
import { WebviewWindow } from "@tauri-apps/api/webviewWindow";
import { getCurrentWindow } from "@tauri-apps/api/window";

export type NativeDialogWindowOptions = Readonly<{
  label: string;
  title: string;
  route: string;
  params: Readonly<Record<string, string>>;
  initialWidth?: number;
  initialHeight?: number;
  maximizable?: boolean;
  resizable?: boolean;
  presentation?: "content" | "pinned";
  chrome?: "app" | "dialog";
}>;

export type NativeDialogOpenResult = "created" | "reused";

export type NativeDialogContentSize = Readonly<{
  width: number;
  height: number;
}>;

export async function openNativeDialogWindow(
  options: NativeDialogWindowOptions,
): Promise<NativeDialogOpenResult> {
  const url = routeWithParams(options.route, options.params);
  const label = options.label.startsWith("native-dialog-") ? options.label : `native-dialog-${options.label}`;
  const existing = await WebviewWindow.getByLabel(label);
  if (existing !== null) {
    await bringDialogToFront(existing, options);
    return "reused";
  }
  const placement = await centeredOnCurrentWindow(options);
  const appearance = await invoke<{ transparency: boolean; lowPower: boolean } | null>(
    "desktop_launch_appearance",
  );
  await new Promise<void>((resolve, reject) => {
    const window = new WebviewWindow(label, {
      alwaysOnTop: options.presentation !== "content",
      center: false,
      closable: true,
      decorations: true,
      focus: false,
      height: placement.height,
      hiddenTitle: true,
      maximizable: options.maximizable ?? false,
      ...(options.presentation === "content" ? {} : { parent: getCurrentWindow() }),
      preventOverflow: true,
      resizable: options.resizable ?? false,
      shadow: true,
      skipTaskbar: options.presentation !== "content",
      title: options.title,
      titleBarStyle: "overlay",
      trafficLightPosition:
        options.chrome === "app" ? new LogicalPosition(12, 22) : new LogicalPosition(20, 18),
      transparent: appearance !== null && appearance.transparency && !appearance.lowPower,
      url,
      visible: false,
      width: placement.width,
      x: placement.x,
      y: placement.y,
    });
    window
      .once("tauri://created", () => {
        prepareNativeDialogWindow(label, window, options).then(resolve, reject);
      })
      .catch(reject);
    window
      .once("tauri://error", (event) => {
        reject(new Error(String(event.payload)));
      })
      .catch(reject);
  });
  return "created";
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

async function prepareNativeDialogWindow(
  label: string,
  window: WebviewWindow,
  options: NativeDialogWindowOptions,
): Promise<void> {
  try {
    await applyNativeWindowGlass(label);
  } catch (error) {
    void appendDialogGlassWarning(label, error);
  } finally {
    await bringDialogToFront(window, options);
  }
}

async function applyNativeWindowGlass(label: string): Promise<void> {
  await invoke("apply_native_window_glass", { label });
}

async function appendDialogGlassWarning(label: string, error: unknown): Promise<void> {
  try {
    await invoke("append_gui_log", {
      entry: JSON.stringify({
        context: { error: nativeErrorMessage(error), label },
        level: "warn",
        message: "Native dialog glass application failed.",
        occurredAt: new Date().toISOString(),
      }),
    });
  } catch {
    return;
  }
}

function nativeErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function bringDialogToFront(window: WebviewWindow, options: NativeDialogWindowOptions): Promise<void> {
  await window.show();
  await window.setAlwaysOnTop(options.presentation !== "content");
  if (options.presentation === "content") await window.unminimize();
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
