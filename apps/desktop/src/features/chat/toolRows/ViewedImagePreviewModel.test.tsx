import { QueryClient } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";

import { deferred } from "@/test-support/chat-runtime";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { ViewedImageRow } from "./ViewedImageRow";
import type { TranscriptToolSlotItem } from "./toolSlotTypes";
import type { ToolPresentation } from "./toolPresentation";

function fixture() {
  const services = createTestServices([]);
  const resolveSource = vi
    .spyOn(services.api.chat, "resolveViewedImageOriginalSource")
    .mockResolvedValue("/viewed-image?source=source&signature=signature");
  const client = new QueryClient();
  return { client, resolveSource, services };
}

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
      AnsweredBySessionID: null,
      ToolCallID: "image",
      ToolName: "view_image",
      Text: "",
      IsError: false,
    },
  },
};

function renderPreview(context: ReturnType<typeof fixture>, strict = false) {
  return render(
    <ViewedImageRow
      presentation={imagePresentation}
      item={imageItem}
      target={{ projectID: "project", sessionID: "session" }}
      effectiveWorkdir={null}
    />,
    {
      wrapper: ({ children }) => {
        const providers = (
          <TestAppProviders services={context.services} queryClient={context.client}>
            {children}
          </TestAppProviders>
        );
        return strict ? <StrictMode>{providers}</StrictMode> : providers;
      },
    },
  );
}

it("resolves the server source only after expansion and mounts one image element", async () => {
  const context = fixture();
  renderPreview(context, true);

  expect(context.resolveSource).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button"));
  await waitFor(() => {
    expect(context.resolveSource).toHaveBeenCalledWith(
      { projectID: "project", sessionID: "session" },
      "/images/image.png",
      expect.any(AbortSignal),
    );
  });
  await waitFor(() => {
    expect(screen.queryAllByAltText("")).toHaveLength(1);
    expect(screen.getByAltText("")).toHaveAttribute(
      "src",
      "http://127.0.0.1:53082/viewed-image?source=source&signature=signature",
    );
  });
});

it("shows an inline failure instead of leaving a broken image", async () => {
  const context = fixture();
  renderPreview(context);
  fireEvent.click(screen.getByRole("button"));
  fireEvent.error(await screen.findByAltText(""));

  await waitFor(() => {
    expect(screen.queryByAltText("")).not.toBeInTheDocument();
  });
  expect(screen.getByRole("button", { expanded: true })).toBeInTheDocument();
});

it("shows source-resolution failures inline in the expanded row", async () => {
  const context = fixture();
  context.resolveSource.mockRejectedValue(new Error("source request failed"));
  renderPreview(context);
  fireEvent.click(screen.getByRole("button"));

  await waitFor(() => {
    expect(context.resolveSource).toHaveBeenCalled();
    expect(screen.queryByAltText("")).not.toBeInTheDocument();
  });
  expect(screen.getByRole("button", { expanded: true })).toBeInTheDocument();
});

it("cancels source resolution when the row collapses", async () => {
  const context = fixture();
  let requestSignal: AbortSignal | undefined;
  context.resolveSource.mockImplementation((_target, _path, signal) => {
    requestSignal = signal;
    return new Promise((_resolve, reject) => {
      signal?.addEventListener("abort", () => reject(new Error("source request cancelled")), {
        once: true,
      });
    });
  });
  renderPreview(context);

  fireEvent.click(screen.getByRole("button"));
  await waitFor(() => expect(requestSignal).toBeDefined());
  fireEvent.click(screen.getByRole("button"));
  await waitFor(() => expect(requestSignal?.aborted).toBe(true));
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
    {
      wrapper: ({ children }) => (
        <TestAppProviders services={services} queryClient={context.client}>
          {children}
        </TestAppProviders>
      ),
    },
  );
  fireEvent.click(await screen.findByRole("button"));
  await waitFor(() => {
    expect(open).toHaveBeenCalledWith({ path: "/images/file.pdf", basePath: undefined });
  });
  expect(available).toHaveBeenCalledWith({ path: "/images/file.pdf", basePath: undefined });
  expect(context.resolveSource).not.toHaveBeenCalled();
});

it("does not let a disposed source resolution replace a reopened row", async () => {
  const context = fixture();
  const oldSource = deferred<string>();
  const currentSource = deferred<string>();
  context.resolveSource.mockReturnValueOnce(oldSource.promise).mockReturnValueOnce(currentSource.promise);
  renderPreview(context);
  const disclosure = screen.getByRole("button");

  fireEvent.click(disclosure);
  await waitFor(() => {
    expect(context.resolveSource).toHaveBeenCalledTimes(1);
  });
  fireEvent.click(disclosure);
  await waitFor(() => {
    expect(screen.queryByAltText("")).not.toBeInTheDocument();
  });
  fireEvent.click(disclosure);
  await waitFor(() => {
    expect(context.resolveSource).toHaveBeenCalledTimes(2);
  });

  await act(async () => {
    oldSource.resolve("/viewed-image?source=old&signature=signature");
    await oldSource.promise;
  });
  expect(screen.queryByAltText("")).not.toBeInTheDocument();

  await act(async () => {
    currentSource.resolve("/viewed-image?source=current&signature=signature");
    await currentSource.promise;
  });
  await waitFor(() => {
    expect(screen.getByAltText("")).toHaveAttribute(
      "src",
      "http://127.0.0.1:53082/viewed-image?source=current&signature=signature",
    );
  });
});
