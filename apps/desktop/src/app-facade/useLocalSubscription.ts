import { useCallback, useEffect, useEffectEvent, useState } from "react";

import type { ApiSubscription } from "@/api";

export function useLocalSubscription(
  identity: string | object | null,
  subscribe: (reportFailure: (error: Error) => void, isActive: () => boolean) => ApiSubscription,
) {
  const [state, setState] = useState<
    Readonly<{
      identity: string | object | null;
      attempt: number;
      error: Error | null;
    }>
  >({ identity, attempt: 0, error: null });
  if (state.identity !== identity) {
    setState({ identity, attempt: 0, error: null });
  }
  const { attempt } = state;
  const retry = useCallback(() => {
    setState((current) => ({ ...current, attempt: current.attempt + 1, error: null }));
  }, []);
  const start = useEffectEvent(subscribe);
  useEffect(() => {
    if (identity === null) return;
    let active = true;
    let failed = false;
    const subscription = start(
      (error) => {
        if (!active || failed) return;
        failed = true;
        setState({ identity, attempt, error });
      },
      () => active,
    );
    return () => {
      active = false;
      subscription.close();
    };
  }, [identity, attempt]);
  return {
    error: state.identity === identity ? state.error : null,
    retry,
  };
}
