import { RegistryProvider } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createViewedImagePreviewModel, useViewedImagePreview } from "./ViewedImagePreviewModel";

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

function fixture(localEnabled = true) {
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
    effectiveWorkdir: "/workspace",
    path: "image.png",
    locator: { event_sequence: 1, row_ordinal: 0 },
  });
  return { client, local, server, model };
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
