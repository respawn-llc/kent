import { useTranslation } from "react-i18next";

export function PromptAccessTargets({
  targets,
}: Readonly<{ targets: readonly Readonly<{ requestedPath: string; resolvedPath: string }>[] }>) {
  const { t } = useTranslation();
  if (targets.length === 0) return null;
  return (
    <>
      <div>{t("task.accessApprovalIntro", { count: targets.length })}</div>
      {targets.map((target, index) => (
        <div
          className="min-w-0 break-words text-[var(--color-on-island)]"
          key={`${String(index)}:${target.requestedPath}`}
        >
          - {target.requestedPath}
          {target.requestedPath === target.resolvedPath ? null : ` → ${target.resolvedPath}`}
        </div>
      ))}
      <div>{t("task.accessApprovalQuestion")}</div>
    </>
  );
}
