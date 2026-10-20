import { useTranslation } from "react-i18next";

type AccessTarget = Readonly<{ requestedPath: string; resolvedPath: string }>;

export function PromptAccessTargets({
  targets,
  formatPath,
}: Readonly<{
  targets: readonly AccessTarget[];
  formatPath: (path: string) => string;
}>) {
  const { t } = useTranslation();
  const first = targets[0];
  if (first === undefined) return null;
  if (targets.length === 1) {
    return (
      <>
        <div className="min-w-0 break-words text-[var(--color-on-island)]">
          {t("task.accessApprovalSingle", { path: displayAccessTarget(first, formatPath) })}
        </div>
        <div>{t("task.accessApprovalSingleQuestion")}</div>
      </>
    );
  }
  return (
    <>
      <div>{t("task.accessApprovalIntro", { count: targets.length })}</div>
      {targets.map((target, index) => (
        <div
          className="min-w-0 break-words text-[var(--color-on-island)]"
          key={`${String(index)}:${target.requestedPath}`}
        >
          - {displayAccessTarget(target, formatPath)}
        </div>
      ))}
      <div>{t("task.accessApprovalQuestion")}</div>
    </>
  );
}

function displayAccessTarget(target: AccessTarget, formatPath: (path: string) => string): string {
  const requested = formatPath(target.requestedPath);
  return target.requestedPath === target.resolvedPath
    ? requested
    : `${requested} → ${formatPath(target.resolvedPath)}`;
}
