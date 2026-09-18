import { createContext, useContext, useMemo } from "react";
import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { boardCardInstanceKey, type BoardCardInstance, type BoardCardInstanceKey } from "./BoardCardInstance";

type Registration = Readonly<{ instance: BoardCardInstance; element: HTMLElement }>;
type RegistrationInput = Readonly<{ instance: BoardCardInstance; element: HTMLElement | null }>;

export function createBoardCardVisibility() {
  const visible = Atom.make<ReadonlySet<BoardCardInstanceKey>>(new Set<BoardCardInstanceKey>());
  const resources = Atom.make((get) => {
    get.mount(visible);
    const registrations = new Map<BoardCardInstanceKey, Registration>();
    const keyByElement = new WeakMap<Element, BoardCardInstanceKey>();
    const observer =
      typeof IntersectionObserver === "undefined"
        ? null
        : new IntersectionObserver((observations) => {
            const next = new Set(get.once(visible));
            for (const observed of observations) {
              const key = keyByElement.get(observed.target);
              if (key === undefined) continue;
              if (observed.isIntersecting) next.add(key);
              else next.delete(key);
            }
            get.set(visible, next);
          });
    get.addFinalizer(() => {
      observer?.disconnect();
      registrations.clear();
    });
    return {
      register({ instance, element }: RegistrationInput) {
        const key = boardCardInstanceKey(instance);
        const previous = registrations.get(key);
        if (previous !== undefined) {
          observer?.unobserve(previous.element);
          keyByElement.delete(previous.element);
        }
        if (element === null) {
          registrations.delete(key);
          const next = new Set(get.once(visible));
          next.delete(key);
          get.set(visible, next);
          return;
        }
        registrations.set(key, { instance, element });
        keyByElement.set(element, key);
        if (observer === null) {
          get.set(visible, new Set([...get.once(visible), key]));
        } else observer.observe(element);
      },
      visibleTaskIDs(): ReadonlySet<string> {
        const result = new Set<string>();
        for (const key of get.once(visible)) {
          const entry = registrations.get(key);
          if (entry !== undefined) result.add(entry.instance.taskID);
        }
        return result;
      },
      elementForUniqueTask(taskID: string): HTMLElement | undefined {
        let unique: HTMLElement | undefined;
        for (const entry of registrations.values()) {
          if (entry.instance.taskID !== taskID) continue;
          if (unique !== undefined) return undefined;
          unique = entry.element;
        }
        return unique;
      },
    };
  });
  const register = Atom.fn<RegistrationInput>()((input, get) =>
    Effect.sync(() => {
      get(resources).register(input);
    }),
  );
  const instanceVisibility = Atom.family((key: BoardCardInstanceKey) =>
    Atom.make((get) => get(visible).has(key)),
  );
  return { resources, register, instanceVisibility } as const;
}

export const BoardCardVisibilityContext = createContext<ReturnType<typeof createBoardCardVisibility> | null>(
  null,
);
const unobservedVisibility = Atom.make(true);

export function useBoardCardInstanceVisibility(instance: BoardCardInstance): boolean {
  const model = useContext(BoardCardVisibilityContext);
  const key = boardCardInstanceKey(instance);
  const visibility = useMemo(() => model?.instanceVisibility(key) ?? unobservedVisibility, [model, key]);
  return useAtomValue(visibility);
}
