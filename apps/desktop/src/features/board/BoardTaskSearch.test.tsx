import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
import {
  createSearchTestServices,
  searchResponse,
  shortIDSearchResponse,
  shortIDContinuationFixture,
  searchRoute,
} from "@/test-support/task-search";

import { appI18n } from "@/i18n";
import { SidebarRootContext, type SidebarRootController } from "@/app-facade";
import { TestAppProviders } from "@/test-support/app-services";
import {
  TaskSearchGlobalTrigger,
  TaskSearchHost,
  TaskSearchProjectTrigger,
  TaskSearchProvider,
} from "./TaskSearchChrome";

const openSidebarRoot = vi.fn<SidebarRootController["open"]>(() => ({
  lifecycle: Promise.resolve("closed" as const),
  release: vi.fn(),
}));
const testSidebarRoots: SidebarRootController = {
  open: openSidebarRoot,
};

describe("Board Task Search", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    openSidebarRoot.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("debounces a Project-scoped Comment-inclusive search", async () => {
    const pendingSearch = new Promise<never>(() => undefined);
    const services = createSearchTestServices([searchRoute(async () => pendingSearch)]);

    renderSearch(services, "project-1");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));

    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    expect(input).toHaveFocus();
    fireEvent.change(input, { target: { value: "search" } });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(299);
    });
    expect(services.searches).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(services.searches.mock.calls.map(([request]) => request)).toMatchObject([
      {
        mode: "literal",
        query: "search",
        context: 20,
        caseSensitive: false,
        includeComments: true,
        projectIDs: ["project-1"],
        pageSize: 40,
      },
    ]);
    expect(services.searches.mock.calls[0]?.[1]).toBeInstanceOf(AbortSignal);
  });

  it("opens global Search from the shortcut without a Project filter", async () => {
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);

    renderSearch(services, null);
    fireEvent.keyDown(window, { code: "KeyS", metaKey: true });

    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    expect(input).toHaveFocus();
    fireEvent.change(input, { target: { value: "search" } });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(services.searches.mock.calls.map(([request]) => request)).toMatchObject([
      {
        mode: "literal",
        query: "search",
        context: 20,
        caseSensitive: false,
        includeComments: true,
        pageSize: 40,
      },
    ]);
    expect(services.searches.mock.calls[0]?.[0].projectIDs ?? []).toEqual([]);
  });

  it("opens a global Search result as an owned sidebar root", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);

    renderSearch(services, null);
    fireEvent.keyDown(window, { code: "KeyS", metaKey: true });
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(openSidebarRoot).toHaveBeenCalledWith({
        kind: "taskDetail",
        mode: "overlay",
        taskID: "task-1",
      });
    });
  });

  it("replaces an open Project Search with the single global dialog", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);

    renderSearch(services, "project-open");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);

    fireEvent.keyDown(window, { code: "KeyS", metaKey: true });

    await waitFor(() => {
      expect(services.searches).toHaveBeenCalledTimes(2);
      expect(services.searches.mock.calls.at(-1)?.[0].projectIDs ?? []).toEqual([]);
    });
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
  });

  it("keeps input focus while arrows choose a Task and Enter opens it", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);
    const onOpenTask = vi.fn();

    renderSearch(services, "project-keyboard", onOpenTask);
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    const listbox = screen.getByRole("listbox", { name: appI18n.t("taskSearch.results") });
    expect(input).toHaveAttribute("aria-controls", listbox.id);
    expect(within(listbox).queryByRole("listitem")).not.toBeInTheDocument();
    expect(screen.getAllByRole("option")[0]).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("img", { name: appI18n.t("taskSearch.commentHit") })).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveFocus();
    expect(screen.getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(input, { key: "Enter" });
    expect(onOpenTask).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(onOpenTask).toHaveBeenCalledWith("task-2");
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("selects the exact numeric Short ID without duplicating its preview", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => shortIDSearchResponse)]);
    const onOpenTask = vi.fn();

    renderSearch(services, "project-short-id", onOpenTask);
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "345" } });
    const options = await screen.findAllByRole("option");
    const exact = options[0];
    if (exact === undefined) {
      throw new Error("Short ID Search requires an exact result.");
    }
    expect(exact).toHaveAttribute("aria-selected", "true");
    expect(within(exact).getByText("KNT-345")).toBeInTheDocument();
    expect(within(exact).queryByText("345")).not.toBeInTheDocument();
    expect(within(exact).getByText("Preview two")).toBeInTheDocument();
    expect(within(exact).getByText("Preview three")).toBeInTheDocument();
    expect(within(exact).getByText("Preview four")).toBeInTheDocument();
    expect(within(exact).queryByText("Preview five")).not.toBeInTheDocument();
    expect(within(exact).getByText("…2 more hits")).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => {
      expect(onOpenTask).toHaveBeenCalledWith("task-exact");
    });
  });

  it("derives continuation previews and remaining hits from that returned group", async () => {
    vi.useRealTimers();
    const continuationResponse = shortIDContinuationFixture();
    const services = createSearchTestServices([searchRoute(() => continuationResponse)]);

    renderSearch(services, "project-continuation");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    fireEvent.change(screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") }), {
      target: { value: "345" },
    });
    const result = await screen.findByRole("option");
    expect(within(result).getByText("Preview two")).toBeInTheDocument();
    expect(within(result).getByText("Preview three")).toBeInTheDocument();
    expect(within(result).getByText("Preview four")).toBeInTheDocument();
    expect(within(result).queryByText("Preview five")).not.toBeInTheDocument();
    expect(within(result).getByText("…2 more hits")).toBeInTheDocument();
  });

  it("retains one query while rerunning Search in the next Project scope", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);
    const view = renderSearch(services, "project-first");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    await waitFor(() => {
      expect(services.searches).toHaveBeenCalledOnce();
    });

    view.rerender(renderProjectSearchTree(services, "project-second"));

    expect(screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") })).toHaveValue("search");
    await waitFor(() => {
      expect(services.searches.mock.calls.at(-1)?.[0]).toMatchObject({
        projectIDs: ["project-second"],
        query: "search",
      });
    });
  });

  it("retains each Project selection while switching Project scope", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);
    const view = renderSearch(services, "project-first");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const firstInput = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(firstInput, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    fireEvent.keyDown(firstInput, { key: "ArrowDown" });
    expect(screen.getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");

    view.rerender(renderProjectSearchTree(services, "project-second"));
    const secondInput = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    await waitFor(() => {
      expect(screen.getAllByRole("option")).toHaveLength(2);
    });
    fireEvent.keyDown(secondInput, { key: "ArrowDown" });
    fireEvent.keyDown(secondInput, { key: "ArrowUp" });
    expect(screen.getAllByRole("option")[0]).toHaveAttribute("aria-selected", "true");

    view.rerender(renderProjectSearchTree(services, "project-first"));
    await waitFor(() => {
      expect(screen.getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");
    });
  });

  it("keeps the prior Task result actionable while a replacement query debounces", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);
    const onOpenTask = vi.fn();

    renderSearch(services, "project-retained", onOpenTask);
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);

    fireEvent.change(input, { target: { value: "replacement query" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(onOpenTask).toHaveBeenCalledWith("task-1");
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("falls back to the first result when a refresh removes the remembered selection", async () => {
    vi.useRealTimers();
    const refreshedResponse = { ...searchResponse, groups: searchResponse.groups.slice(0, 1) };
    const services = createSearchTestServices([
      searchRoute((callIndex) => (callIndex === 0 ? searchResponse : refreshedResponse)),
    ]);

    renderSearch(services, "project-refresh");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(screen.getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));

    await waitFor(() => {
      expect(services.searches).toHaveBeenCalledTimes(2);
      expect(screen.getAllByRole("option")).toHaveLength(1);
    });
    expect(screen.getByRole("option")).toHaveAttribute("aria-selected", "true");
  });

  it("cancels a pending Task activation when Search reopens during exit", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);
    const onOpenTask = vi.fn();

    renderSearch(services, null, onOpenTask);
    fireEvent.keyDown(window, { code: "KeyS", metaKey: true });
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    expect(await screen.findAllByRole("option")).toHaveLength(2);

    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.keyDown(window, { code: "KeyS", metaKey: true });
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    expect(onOpenTask).not.toHaveBeenCalled();
  });

  it("selects on pointer movement but ignores stationary pointer events after keyboard selection", async () => {
    vi.useRealTimers();
    const services = createSearchTestServices([searchRoute(() => searchResponse)]);

    renderSearch(services, "project-pointer-intent");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("taskSearch.open") }));
    const input = screen.getByRole("searchbox", { name: appI18n.t("taskSearch.input") });
    fireEvent.change(input, { target: { value: "search" } });
    const options = await screen.findAllByRole("option");
    const first = options[0];
    const second = options[1];
    if (first === undefined || second === undefined) {
      throw new Error("Task Search pointer test requires two results.");
    }

    fireEvent.pointerMove(second, { pointerType: "mouse", clientX: 10, clientY: 10 });
    expect(second).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(first).toHaveAttribute("aria-selected", "true");

    fireEvent.pointerMove(second, { pointerType: "mouse", clientX: 10, clientY: 10 });
    expect(first).toHaveAttribute("aria-selected", "true");

    fireEvent.pointerMove(second, { pointerType: "mouse", clientX: 11, clientY: 10 });
    expect(second).toHaveAttribute("aria-selected", "true");
  });
});

function renderSearch(
  services: ReturnType<typeof createSearchTestServices>,
  projectID: string | null,
  onOpenTask = vi.fn(),
): ReturnType<typeof render> {
  const search =
    projectID === null ? (
      <TaskSearchGlobalTrigger />
    ) : (
      <TaskSearchProjectTrigger onOpenTask={onOpenTask} projectID={projectID} />
    );
  return render(
    <TestAppProviders services={services}>
      <TaskSearchProvider>
        <SidebarRootContext.Provider value={testSidebarRoots}>
          <TaskSearchHost />
          {search}
        </SidebarRootContext.Provider>
      </TaskSearchProvider>
    </TestAppProviders>,
  );
}

function renderProjectSearchTree(
  services: ReturnType<typeof createSearchTestServices>,
  projectID: string,
): ReactElement {
  return (
    <TestAppProviders services={services}>
      <TaskSearchProvider>
        <SidebarRootContext.Provider value={testSidebarRoots}>
          <TaskSearchHost />
          <TaskSearchProjectTrigger onOpenTask={vi.fn()} projectID={projectID} />
        </SidebarRootContext.Provider>
      </TaskSearchProvider>
    </TestAppProviders>
  );
}
