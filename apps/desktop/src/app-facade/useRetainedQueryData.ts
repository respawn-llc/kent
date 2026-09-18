import { useState } from "react";

export type RetainedQueryData<TData, TScope> = Readonly<{
  data: TData;
  scope: TScope;
}>;

export function retainQueryData<TData, TScope>(
  retained: RetainedQueryData<TData, TScope> | null,
  input: Readonly<{ scope: TScope; data: TData | undefined; retain: boolean }>,
  scopesEqual: (left: TScope, right: TScope) => boolean,
) {
  const { scope, data, retain } = input;
  const next = !retain
    ? null
    : data !== undefined && (retained?.data !== data || !scopesEqual(retained.scope, scope))
      ? { data, scope }
      : retained;
  return { retained: next, data: next !== null && scopesEqual(next.scope, scope) ? next.data : undefined };
}

export function useRetainedQueryData<TData, TScope>(
  scope: TScope,
  data: TData | undefined,
  scopesEqual: (left: TScope, right: TScope) => boolean,
  retain = true,
): TData | undefined {
  const [retained, setRetained] = useState<RetainedQueryData<TData, TScope> | null>(null);
  const next = retainQueryData(retained, { scope, data, retain }, scopesEqual);
  if (next.retained !== retained) setRetained(next.retained);
  return next.data;
}
