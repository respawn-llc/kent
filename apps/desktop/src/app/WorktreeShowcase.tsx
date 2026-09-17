import { lazy, Suspense } from "react";
import { Spinner } from "@/ui";

const View = lazy(async () => {
  const { WorktreeShowcaseView } = await import("./WorktreeShowcaseView");
  return { default: WorktreeShowcaseView };
});

export function WorktreeShowcase() {
  return (
    <Suspense fallback={<Spinner />}>
      <View />
    </Suspense>
  );
}
