import { RegistryProvider } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createViewedImagePreviewModel, useViewedImagePreview } from "./ViewedImagePreviewModel";
import { ViewedImageRow } from "./ViewedImageRow";
import type { TranscriptToolSlotItem } from "./toolSlotTypes";
import type { ToolPresentation } from "./toolPresentation";

const decode = vi.fn(async (): Promise<void> => undefined);
const createURL = vi.fn(() => "blob:preview");
const revokeURL = vi.fn();

beforeEach(() => {
  decode.mockReset().mockResolvedValue(undefined);
  createURL.mockReset().mockReturnValue("blob:preview");
  revokeURL.mockReset();
  vi.stubGlobal(
    "Image",
    class {
      src = "";
      naturalWidth = 25;
      naturalHeight = 50;
      decode = decode;
    },
  );
  vi.stubGlobal(
    "URL",
    class extends URL {
      static override createObjectURL = createURL;
      static override revokeObjectURL = revokeURL;
    },
  );
});

afterEach(() => vi.unstubAllGlobals());

function fixture(localEnabled = true, effectiveWorkdir: string | null = "/workspace", path = "image.png") {
  const services = createTestServices([]);
  const server = vi.spyOn(services.api.chat, "getViewedImageOriginal").mockResolvedValue({
    kind: "content",
    bytes: new Uint8Array([2]),
  });
  const local = vi.fn(async (): Promise<Uint8Array> => new Uint8Array([1]));
  const nativeBridge = {
    ...services.nativeBridge,
    files: { ...services.nativeBridge.files, readImage: localEnabled ? local : null },
  };
  const client = new QueryClient();
  const model = createViewedImagePreviewModel({
    services: { ...services, nativeBridge },
    client,
    target: { projectID: "project", sessionID: "session" },
    effectiveWorkdir,
    path,
    locator: { event_sequence: 1, row_ordinal: 0 },
  });
  return { client, local, server, model, services: { ...services, nativeBridge } };
}

function mount(context: ReturnType<typeof fixture>) {
  return renderHook(() => useViewedImagePreview(context.model), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
}

it("uses a decoded local original without transferring server bytes", async () => {
  const context = fixture();
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data?.kind).toBe("image");
  });
  expect(view.result.current.data).toMatchObject({ source: "local", width: 25, height: 50 });
  expect(context.server).not.toHaveBeenCalled();
  view.unmount();
  await waitFor(() => {
    expect(revokeURL).toHaveBeenCalledWith("blob:preview");
  });
});

it("uses the server original without local path context", async () => {
  const context = fixture(true, null);
  context.local.mockRejectedValue(new Error("Relative path requires a base directory"));
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "server" });
  });
});

it("uses an absolute local original without a working directory", async () => {
  const context = fixture(true, null, "/images/image.png");
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "local" });
  });
  expect(context.server).not.toHaveBeenCalled();
});

const imagePresentation: Extract<ToolPresentation, { kind: "view-image" }> = {
  kind: "view-image",
  body: [],
  compact: "/images/image.png",
  path: "/images/image.png",
  outcome: "raster",
  icon: "image",
  iconTone: "neutral",
  running: false,
};
const imageItem: TranscriptToolSlotItem = {
  kind: "committed",
  row: {
    Kind: "tool",
    Visibility: "ongoing",
    Integrity: 0,
    Locator: { event_sequence: 1, row_ordinal: 0 },
    User: null,
    Assistant: null,
    ReasoningTrace: null,
    Notice: null,
    ReviewerFeedback: null,
    ReviewerError: null,
    Tool: {
      ViewedImageKind: "raster",
      ToolCallID: "image",
      ToolName: "view_image",
      Text: "",
      IsError: false,
    },
  },
};

it("finishes an expanded acquisition when working-directory context hydrates", async () => {
  const context = fixture(false, null);
  const read = deferred<Awaited<ReturnType<typeof context.server>>>();
  context.server.mockReturnValueOnce(read.promise);
  const row = (effectiveWorkdir: string | null) => (
    <ViewedImageRow
      presentation={imagePresentation}
      item={imageItem}
      target={{ projectID: "project", sessionID: "session" }}
      effectiveWorkdir={effectiveWorkdir}
    />
  );
  const view = render(row(null), {
    wrapper: ({ children }) => (
      <TestAppProviders services={context.services} queryClient={context.client}>
        {children}
      </TestAppProviders>
    ),
  });
  fireEvent.click(screen.getByRole("button"));
  await waitFor(() => {
    expect(context.server).toHaveBeenCalledTimes(1);
  });
  view.rerender(row("/workspace"));
  await act(async () => {
    read.resolve({ kind: "content", bytes: new Uint8Array([3]) });
  });
  await waitFor(() => {
    expect(screen.getByAltText("")).toHaveAttribute("src", "blob:preview");
  });
});

it("offers native Open for an available absolute PDF without a working directory", async () => {
  const context = fixture();
  const available = vi.spyOn(context.services.nativeBridge.files, "fileAvailable").mockResolvedValue(true);
  const open = vi.spyOn(context.services.nativeBridge.files, "openDefault").mockResolvedValue();
  const services = {
    ...context.services,
    nativeBridge: {
      ...context.services.nativeBridge,
      capabilities: {
        ...context.services.nativeBridge.capabilities,
        files: { ...context.services.nativeBridge.capabilities.files, stat: true, open: true },
      },
    },
  };
  render(
    <ViewedImageRow
      presentation={{ ...imagePresentation, path: "/images/file.pdf", outcome: "pdf" }}
      item={imageItem}
      target={{ projectID: "project", sessionID: "session" }}
      effectiveWorkdir={null}
    />,
    { wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders> },
  );
  const button = await screen.findByRole("button");
  fireEvent.click(button);
  await waitFor(() => {
    expect(open).toHaveBeenCalledWith({ path: "/images/file.pdf", basePath: undefined });
  });
  expect(available).toHaveBeenCalledWith({ path: "/images/file.pdf", basePath: undefined });
  expect(context.server).not.toHaveBeenCalled();
});

it("uses the server original when local acquisition fails", async () => {
  const context = fixture();
  context.local.mockRejectedValue(new Error("local file unavailable"));
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data?.kind).toBe("image");
  });
  expect(view.result.current.data).toMatchObject({ source: "server" });
});

it("falls back to the server when the local original cannot decode", async () => {
  const context = fixture();
  decode.mockRejectedValueOnce(new Error("corrupt local image"));
  createURL.mockReturnValueOnce("blob:local").mockReturnValueOnce("blob:server");
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "server" });
  });
  expect(revokeURL).toHaveBeenCalledWith("blob:local");
  expect(revokeURL).not.toHaveBeenCalledWith("blob:server");
});

it("shows unavailable when the server original is missing", async () => {
  const context = fixture();
  context.local.mockRejectedValue(new Error("local unavailable"));
  context.server.mockResolvedValue({ kind: "unavailable" });
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toEqual({ kind: "unavailable" });
  });
  expect(createURL).not.toHaveBeenCalled();
});

it("reports an operational server failure as a request error", async () => {
  const context = fixture();
  context.local.mockRejectedValue(new Error("local unavailable"));
  const failure = new Error("connection timeout");
  context.server.mockRejectedValue(failure);
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.isError).toBe(true);
  });
  expect(view.result.current.error).toBe(failure);
});

it("re-expands with a fresh acquisition before a disposed native read completes", async () => {
  const context = fixture();
  const oldRead = deferred<Uint8Array>();
  const freshRead = deferred<Uint8Array>();
  context.local.mockReturnValueOnce(oldRead.promise).mockReturnValueOnce(freshRead.promise);
  const utils = mount(context);
  await waitFor(() => {
    expect(context.local).toHaveBeenCalledTimes(1);
  });
  utils.unmount();
  const view = mount(context);
  await waitFor(() => {
    expect(context.local).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    oldRead.resolve(new Uint8Array([3]));
  });
  expect(view.result.current.isPending).toBe(true);
  expect(createURL).not.toHaveBeenCalled();
  expect(context.server).not.toHaveBeenCalled();
  await act(async () => {
    freshRead.resolve(new Uint8Array([4]));
  });
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "local" });
  });
});

it("uses server-only acquisition in a browser without touching native files", async () => {
  const context = fixture(false);
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "server" });
  });
  expect(context.local).not.toHaveBeenCalled();
});

it("shows unavailable and releases the candidate when the server original cannot decode", async () => {
  const context = fixture(false);
  decode.mockRejectedValue(new Error("unsupported image"));
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toEqual({ kind: "unavailable" });
  });
  expect(revokeURL).toHaveBeenCalledWith("blob:preview");
});

it("discards a disposed server read and reacquires before its completion", async () => {
  const context = fixture(false);
  const oldRead = deferred<Awaited<ReturnType<typeof context.server>>>();
  const freshRead = deferred<Awaited<ReturnType<typeof context.server>>>();
  context.server.mockReturnValueOnce(oldRead.promise).mockReturnValueOnce(freshRead.promise);
  const utils = mount(context);
  await waitFor(() => {
    expect(context.server).toHaveBeenCalledTimes(1);
  });
  utils.unmount();
  const view = mount(context);
  await waitFor(() => {
    expect(context.server).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    oldRead.resolve({ kind: "content", bytes: new Uint8Array([3]) });
  });
  expect(view.result.current.isPending).toBe(true);
  expect(createURL).not.toHaveBeenCalled();
  await act(async () => {
    freshRead.resolve({ kind: "content", bytes: new Uint8Array([4]) });
  });
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ kind: "image", source: "server" });
  });
});

it("releases a decoding candidate on disposal and ignores its later failure", async () => {
  const context = fixture();
  const oldDecode = deferred<undefined>();
  decode.mockReturnValueOnce(oldDecode.promise);
  createURL.mockReturnValueOnce("blob:old").mockReturnValueOnce("blob:fresh");
  const utils = mount(context);
  await waitFor(() => {
    expect(decode).toHaveBeenCalledTimes(1);
  });
  utils.unmount();
  const view = mount(context);
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ url: "blob:fresh" });
  });
  expect(revokeURL).toHaveBeenCalledWith("blob:old");
  await act(async () => {
    oldDecode.reject(new Error("late decode failure"));
  });
  expect(view.result.current.data).toMatchObject({ url: "blob:fresh" });
  expect(context.server).not.toHaveBeenCalled();
  expect(revokeURL).not.toHaveBeenCalledWith("blob:fresh");
});

it("re-expands a completed row without retaining its old preview", async () => {
  const context = fixture();
  createURL.mockReturnValueOnce("blob:old").mockReturnValueOnce("blob:fresh");
  const utils = mount(context);
  await waitFor(() => {
    expect(utils.result.current.data).toMatchObject({ url: "blob:old" });
  });
  utils.unmount();
  const freshRead = deferred<Uint8Array>();
  context.local.mockReturnValueOnce(freshRead.promise);
  const view = mount(context);
  expect(view.result.current.data).toBeUndefined();
  await waitFor(() => {
    expect(context.local).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    freshRead.resolve(new Uint8Array([4]));
  });
  await waitFor(() => {
    expect(view.result.current.data).toMatchObject({ url: "blob:fresh" });
  });
});
