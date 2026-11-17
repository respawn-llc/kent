import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "bun:test";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { WorkflowParameterOutput } from "./WorkflowParameterOutput";

it("retains Markdown interaction and path copying without native inspection in a browser", async () => {
  const base = createTestServices([]);
  const inspect = vi.fn(base.nativeBridge.files.inspectOutputFile);
  const read = vi.fn(base.nativeBridge.files.readOutputImage);
  const write = vi.fn(async () => undefined);
  const services = {
    ...base,
    nativeBridge: {
      ...base.nativeBridge,
      capabilities: { ...base.nativeBridge.capabilities, clipboard: { writeText: true, readText: false } },
      files: { ...base.nativeBridge.files, inspectOutputFile: inspect, readOutputImage: read },
      clipboard: { ...base.nativeBridge.clipboard, writeText: write },
    },
  };
  render(
    <TestAppProviders services={services}>
      <WorkflowParameterOutput
        root={{ kind: "unavailable" }}
        value={"**Result** [web](https://example.com) ![photo](./a%2520%23%3F.png)"}
      />
    </TestAppProviders>,
  );
  expect(screen.getByRole("link").getAttribute("href")).toBe("https://example.com/");
  await userEvent.click(screen.getByRole("button"));
  await waitFor(() => {
    expect(write).toHaveBeenCalledWith("./a%20#?.png");
  });
  expect(inspect).not.toHaveBeenCalled();
  expect(read).not.toHaveBeenCalled();
});

it("does not probe or open a relative path when the managed root is unavailable", async () => {
  const base = createTestServices([]);
  const inspect = vi.fn(base.nativeBridge.files.inspectOutputFile);
  const read = vi.fn(base.nativeBridge.files.readOutputImage);
  const open = vi.fn(base.nativeBridge.files.openDefault);
  const write = vi.fn(async () => undefined);
  const services = {
    ...base,
    nativeBridge: {
      ...base.nativeBridge,
      capabilities: {
        ...base.nativeBridge.capabilities,
        files: { ...base.nativeBridge.capabilities.files, stat: true, open: true },
        clipboard: { writeText: true, readText: false },
      },
      files: {
        ...base.nativeBridge.files,
        inspectOutputFile: inspect,
        readOutputImage: read,
        openDefault: open,
      },
      clipboard: { ...base.nativeBridge.clipboard, writeText: write },
    },
  };
  render(
    <TestAppProviders services={services}>
      <WorkflowParameterOutput root={{ kind: "unavailable" }} value="[file](./source.txt)" />
    </TestAppProviders>,
  );
  await userEvent.click(screen.getByRole("button"));
  await waitFor(() => {
    expect(write).toHaveBeenCalledWith("./source.txt");
  });
  expect(inspect).not.toHaveBeenCalled();
  expect(read).not.toHaveBeenCalled();
  expect(open).not.toHaveBeenCalled();
});
