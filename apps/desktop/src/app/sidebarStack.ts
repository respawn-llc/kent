import { createElement, Fragment, type ComponentType, type ReactNode } from "react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Option from "effect/Option";
import type {
  SidebarBackResult,
  SidebarDestination,
  SidebarDestinationPolicy,
  SidebarNavigationOutcome,
  SidebarPageNavigator,
  SidebarPhase,
  SidebarRootHandle,
  SidebarRootOutcome,
  SidebarTransitionDirection,
} from "@/app-facade";
const stackLimit = 50;
const exitAnimationMs = 140;
interface Capability {
  readonly id: symbol;
  readonly availability: Readonly<{ back: boolean; close: boolean }> | null;
  readonly capture: Readonly<{ read: () => unknown }> | null;
}
interface PendingRoot {
  resolve(outcome: SidebarRootOutcome): void;
}
export interface SidebarStackEntry {
  readonly Boundary: ComponentType<Readonly<{ children: ReactNode }>>;
  readonly capability: Capability;
  readonly destination: SidebarDestination;
  readonly navigator: SidebarPageNavigator;
  readonly retainedState?: unknown;
}
export interface SidebarStackView {
  readonly entries: readonly SidebarStackEntry[];
  readonly phase: SidebarPhase;
  readonly transitionDirection: SidebarTransitionDirection | null;
}

export const emptySidebarStackView: SidebarStackView = {
  entries: [],
  phase: "open",
  transitionDirection: null,
};
export function createSidebarStack(policy: SidebarDestinationPolicy) {
  const state = Atom.make(emptySidebarStackView);
  const controls = Atom.make((get) => {
    get.mount(state);
    const view = () => get.once(state);
    let root: PendingRoot | undefined;
    let closeTimeout: ReturnType<typeof setTimeout> | undefined;
    const emit = (
      entries: readonly SidebarStackEntry[],
      phase: SidebarPhase,
      direction: SidebarTransitionDirection | null,
    ) => {
      get.set(state, { entries, phase, transitionDirection: direction });
    };
    const current = () => (Option.isNone(get.self()) ? undefined : view().entries.at(-1));
    const accepts = (id: symbol) => current()?.capability.id === id && view().phase === "open";
    const settle = (pending: PendingRoot, outcome: SidebarRootOutcome) => {
      if (root !== pending) return;
      root = undefined;
      pending.resolve(outcome);
    };
    const clearCloseTimeout = () => {
      if (closeTimeout !== undefined) {
        clearTimeout(closeTimeout);
        closeTimeout = undefined;
      }
    };
    const updateCapability = (id: symbol, update: (capability: Capability) => Capability) => {
      if (!accepts(id)) return;
      const snapshot = view();
      emit(
        snapshot.entries.map((entry) =>
          entry.capability.id === id ? { ...entry, capability: update(entry.capability) } : entry,
        ),
        snapshot.phase,
        snapshot.transitionDirection,
      );
    };
    const createEntry = (destination: SidebarDestination, retainedState?: unknown): SidebarStackEntry => {
      const id = Symbol("sidebar-page");
      const capability: Capability = { id, availability: null, capture: null };
      const Boundary = ({ children }: Readonly<{ children: ReactNode }>) =>
        createElement(Fragment, null, children);
      const navigator: SidebarPageNavigator = {
        back: (result) => back(id, result),
        close: () => close(id),
        push: (next) => push(id, next),
        replace: (next) => replace(id, next),
        registerAvailability: (availability) => {
          const registration = { ...availability };
          updateCapability(id, (current) => ({ ...current, availability: registration }));
          return () => {
            updateCapability(id, (current) =>
              current.availability === registration ? { ...current, availability: null } : current,
            );
          };
        },
        registerCapture: (read) => {
          const registration = { read };
          updateCapability(id, (current) => ({ ...current, capture: registration }));
          return () => {
            updateCapability(id, (current) =>
              current.capture === registration ? { ...current, capture: null } : current,
            );
          };
        },
      };
      const entry = { Boundary, capability, destination, navigator };
      return retainedState === undefined
        ? entry
        : { ...entry, retainedState: policy.retainedState(destination, retainedState) };
    };
    const close = (id: symbol): Exclude<SidebarNavigationOutcome, "unavailable"> => {
      if (!accepts(id)) {
        return "stale";
      }
      if (root !== undefined) {
        settle(root, "closed");
      }
      clearCloseTimeout();
      emit(view().entries, "closing", null);
      closeTimeout = setTimeout(() => {
        closeTimeout = undefined;
        emit([], "open", null);
      }, exitAnimationMs);
      return "accepted";
    };
    const back = (
      id: symbol,
      result?: SidebarBackResult,
    ): Exclude<SidebarNavigationOutcome, "unavailable"> => {
      if (!accepts(id)) {
        return "stale";
      }
      if (view().entries.length === 1) {
        return close(id);
      }
      const previous = view().entries.at(-2);
      if (previous === undefined) {
        throw new Error("Sidebar Back requires a previous page.");
      }
      const retainedState =
        result === undefined
          ? previous.retainedState
          : policy.applyBackResult(previous.destination, previous.retainedState, result);
      emit(
        [...view().entries.slice(0, -2), createEntry(previous.destination, retainedState)],
        "open",
        "back",
      );
      return "accepted";
    };
    const replace = (
      id: symbol,
      destination: SidebarDestination,
    ): Exclude<SidebarNavigationOutcome, "unavailable"> => {
      if (!accepts(id)) {
        return "stale";
      }
      const currentDestination = current()?.destination;
      if (currentDestination === undefined) {
        throw new Error("Sidebar Replace requires a current page.");
      }
      emit(
        [
          ...view().entries.slice(0, -1),
          createEntry(destinationInOpenSidebarMode(destination, currentDestination)),
        ],
        "open",
        "replace",
      );
      return "accepted";
    };
    const push = (id: symbol, destination: SidebarDestination): SidebarNavigationOutcome => {
      if (!accepts(id)) {
        return "stale";
      }
      const currentEntry = current();
      if (currentEntry === undefined) {
        throw new Error("Sidebar Push requires a current page.");
      }
      const resolvedDestination = destinationInOpenSidebarMode(destination, currentEntry.destination);
      const retained = findRetainedEntry(view().entries, resolvedDestination, policy);
      if (retained !== undefined) {
        emit(
          [
            ...view().entries.slice(0, retained.index),
            createEntry(retained.entry.destination, retained.entry.retainedState),
          ],
          "open",
          "back",
        );
        return "accepted";
      }
      if (currentEntry.capability.capture === null) {
        return "unavailable";
      }
      const retainedState = currentEntry.capability.capture.read();
      const appended = [
        ...view().entries.slice(0, -1),
        { ...currentEntry, retainedState },
        createEntry(resolvedDestination),
      ];
      const rootEntry = appended[0];
      if (rootEntry === undefined) {
        throw new Error("Sidebar Push lost its root page.");
      }
      emit(
        appended.length <= stackLimit ? appended : [rootEntry, ...appended.slice(-(stackLimit - 1))],
        "open",
        "push",
      );
      return "accepted";
    };
    const open = (destination: SidebarDestination): SidebarRootHandle => {
      clearCloseTimeout();
      const resolvedDestination = destinationInOpenSidebarMode(destination, current()?.destination);
      if (root !== undefined) {
        settle(root, "replaced");
      }
      let resolveLifecycle: ((outcome: SidebarRootOutcome) => void) | undefined;
      const lifecycle = new Promise<SidebarRootOutcome>((resolve) => {
        resolveLifecycle = resolve;
      });
      if (resolveLifecycle === undefined) {
        throw new Error("Sidebar lifecycle promise did not initialize.");
      }
      const ownedRoot: PendingRoot = { resolve: resolveLifecycle };
      root = ownedRoot;
      emit([createEntry(resolvedDestination)], "open", "replace");
      return {
        lifecycle,
        release: () => {
          if (root !== ownedRoot) {
            return;
          }
          settle(ownedRoot, "released");
          clearCloseTimeout();
          emit([], "open", null);
        },
      };
    };
    get.addFinalizer(() => {
      clearCloseTimeout();
      if (root !== undefined) {
        settle(root, "released");
      }
    });
    return {
      currentSurface: () =>
        current() === undefined || view().phase === "closing" ? null : (current()?.destination ?? null),
      open,
    };
  });
  return { view: Atom.make((get) => get(state)), controls };
}

function destinationInOpenSidebarMode(
  destination: SidebarDestination,
  openDestination: SidebarDestination | undefined,
): SidebarDestination {
  if (openDestination === undefined) {
    return destination;
  }
  const openMode = openDestination.mode ?? "shift";
  if ((destination.mode ?? "shift") === openMode) {
    return destination;
  }
  return { ...destination, mode: openMode };
}

function findRetainedEntry(
  entries: readonly SidebarStackEntry[],
  destination: SidebarDestination,
  policy: SidebarDestinationPolicy,
): Readonly<{ entry: SidebarStackEntry; index: number }> | undefined {
  for (const [index, entry] of entries.slice(0, -1).entries()) {
    if (policy.equals(entry.destination, destination)) return { entry, index };
  }
  return undefined;
}
