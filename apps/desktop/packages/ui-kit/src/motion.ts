import { createContext, useContext, useEffect, useState } from "react";

const reducedMotionQuery = "(prefers-reduced-motion: reduce)";
export const ReducedMotionContext = createContext(false);

export function useSystemReducedMotion(): boolean {
  const [reducedMotion, setReducedMotion] = useState(
    () => window.matchMedia instanceof Function && window.matchMedia(reducedMotionQuery).matches,
  );
  useEffect(() => {
    if (!(window.matchMedia instanceof Function)) {
      return undefined;
    }
    const media = window.matchMedia(reducedMotionQuery);
    const handleChange = () => {
      setReducedMotion(media.matches);
    };
    media.addEventListener("change", handleChange);
    return () => {
      media.removeEventListener("change", handleChange);
    };
  }, []);
  return reducedMotion;
}

export function useReducedMotion(): boolean {
  const requested = useContext(ReducedMotionContext);
  return useSystemReducedMotion() || requested;
}

export function prefersReducedMotion(): boolean {
  return (
    (typeof document !== "undefined" && document.documentElement.dataset.reducedMotion === "true") ||
    (globalThis.matchMedia instanceof Function && globalThis.matchMedia(reducedMotionQuery).matches)
  );
}
