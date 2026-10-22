import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { CopyableValue } from "@/shared/copyable-value";
import {
  taskDetailCopyValueNoticePolicy,
  type TaskDetailCopyValueKind,
  type TaskDetailCopyValueLocalization,
} from "./taskDetailCopyValuePolicy";

export function TaskDetailCopyableValue({
  children,
  className,
  clipboardValue,
  kind,
}: Readonly<{
  children: ReactNode;
  className?: string;
  clipboardValue: string;
  kind: TaskDetailCopyValueKind;
}>) {
  const { t } = useTranslation();
  const policy = taskDetailCopyValueNoticePolicy(kind);
  return (
    <CopyableValue
      accessibleLabel={localize(policy.copyLabel, t)}
      clipboardValue={clipboardValue}
      copySucceeded={{ id: policy.success.id, title: localize(policy.success, t) }}
      copyFailed={{ id: policy.failure.id, title: localize(policy.failure, t) }}
      {...(className === undefined ? {} : { className })}
    >
      {children}
    </CopyableValue>
  );
}

function localize(
  selection: TaskDetailCopyValueLocalization,
  t: ReturnType<typeof useTranslation>["t"],
): string {
  return "interpolation" in selection
    ? t(selection.titleKey, selection.interpolation)
    : t(selection.titleKey);
}
