import { state } from "./forbidden-effect-subscription";

export function model() {
  return {
    state,
    subscribe(listener: () => void) {
      listener();
      return () => false;
    },
  };
}
