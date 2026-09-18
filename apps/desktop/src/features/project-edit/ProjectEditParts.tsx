import { type CSSProperties, type KeyboardEventHandler } from "react";
import { useTranslation } from "react-i18next";
import { Link2Off, Star, Unlink } from "lucide-react";

import type { WorkspaceCatalogRow } from "@/api";
import { formatHomeRelativePath } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { Button, Dialog, fieldInputClassName, fieldLabelClassName, islandSurfaceClassName } from "@/ui";
import { cx } from "@/ui";

const workspaceUnlinkDialogWidth = 400;

type WorkspaceUnlinkDialogStyle = CSSProperties & Readonly<Record<"--workspace-unlink-dialog-width", string>>;

const workspaceUnlinkDialogStyle: WorkspaceUnlinkDialogStyle = {
  "--workspace-unlink-dialog-width": `${workspaceUnlinkDialogWidth.toString()}px`,
};

export type WorkspaceUnlinkTarget = Readonly<{
  workspaceID: string;
  rootPath: string;
}>;

export function ProjectNameField({
  disabled,
  nameDraft,
  nameErrors,
  onKeyDown,
  onNameChange,
}: Readonly<{
  disabled: boolean;
  nameDraft: string;
  nameErrors: readonly string[];
  onKeyDown: KeyboardEventHandler<HTMLInputElement>;
  onNameChange: (value: string) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      <label className={fieldLabelClassName} htmlFor="project-edit-name">
        {t("projectEdit.name")}
      </label>
      <input
        aria-describedby="project-edit-name-error"
        aria-invalid={nameErrors.length > 0 ? true : undefined}
        className={fieldInputClassName}
        disabled={disabled}
        id="project-edit-name"
        onChange={(event) => {
          onNameChange(event.target.value);
        }}
        onKeyDown={onKeyDown}
        value={nameDraft}
      />
      <span
        aria-live="polite"
        className="grid overflow-hidden opacity-0 transition-[grid-template-rows,opacity] duration-[var(--motion-normal)] data-[visible=true]:grid-rows-[1fr] data-[visible=true]:opacity-100 grid-rows-[0fr]"
        data-visible={nameErrors.length > 0 ? "true" : "false"}
        id="project-edit-name-error"
      >
        <span className="grid min-h-0 gap-[var(--space-1)]">
          {nameErrors.map((message) => (
            <span className="text-[var(--color-error)]" key={message}>
              {message}
            </span>
          ))}
        </span>
      </span>
    </div>
  );
}

export function ProjectKeyField({
  disabled,
  keyDraft,
  keyErrors,
  onKeyDown,
  onKeyChange,
}: Readonly<{
  disabled: boolean;
  keyDraft: string;
  keyErrors: readonly string[];
  onKeyDown: KeyboardEventHandler<HTMLInputElement>;
  onKeyChange: (value: string) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      <label className={fieldLabelClassName} htmlFor="project-edit-key">
        {t("projectEdit.taskKey")}
      </label>
      <input
        aria-describedby="project-edit-key-error"
        aria-invalid={keyErrors.length > 0 ? true : undefined}
        autoCapitalize="characters"
        className={fieldInputClassName}
        disabled={disabled}
        id="project-edit-key"
        onChange={(event) => {
          onKeyChange(event.target.value.toUpperCase());
        }}
        onKeyDown={onKeyDown}
        spellCheck={false}
        value={keyDraft}
      />
      <span
        aria-live="polite"
        className="grid overflow-hidden opacity-0 transition-[grid-template-rows,opacity] duration-[var(--motion-normal)] data-[visible=true]:grid-rows-[1fr] data-[visible=true]:opacity-100 grid-rows-[0fr]"
        data-visible={keyErrors.length > 0 ? "true" : "false"}
        id="project-edit-key-error"
      >
        <span className="grid min-h-0 gap-[var(--space-1)]">
          {keyErrors.map((message) => (
            <span className="text-[var(--color-error)]" key={message}>
              {message}
            </span>
          ))}
        </span>
      </span>
    </div>
  );
}

export function WorkspaceRow({
  disabled,
  onMakeDefault,
  onUnlink,
  workspace,
}: Readonly<{
  disabled: boolean;
  onMakeDefault: () => void;
  onUnlink: () => void;
  workspace: WorkspaceCatalogRow;
}>) {
  const { t } = useTranslation();
  const { homePath, nativeBridge } = useAppServices();
  const isDefault = workspace.isDefault;
  const workspacePathLabel = formatHomeRelativePath(
    workspace.rootPath,
    homePath,
    nativeBridge.capabilities.platform,
  );
  return (
    <article
      className={cx(
        "grid min-w-0 grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-[var(--space-2)] rounded-[var(--radius-l)] p-[var(--space-3)]",
        islandSurfaceClassName(1),
      )}
      data-testid="workspace-row"
    >
      <span className="min-w-0 truncate font-mono text-sm" title={workspace.rootPath}>
        {workspacePathLabel}
      </span>
      <button
        aria-label={t("projectEdit.makeDefaultWorkspace", { path: workspacePathLabel })}
        aria-pressed={isDefault}
        className={cx(
          "grid h-9 w-9 place-items-center border border-transparent bg-transparent transition-colors duration-[var(--motion-fast)] disabled:cursor-not-allowed disabled:opacity-55",
          isDefault ? "text-[var(--color-secondary)] opacity-100" : "text-[var(--color-muted)]",
        )}
        disabled={disabled}
        onClick={onMakeDefault}
        title={isDefault ? t("projectEdit.default") : t("projectEdit.makeDefault")}
        type="button"
      >
        <Star
          aria-hidden="true"
          className="transition-[color,fill,opacity,transform] duration-[var(--motion-normal)]"
          fill={isDefault ? "currentColor" : "none"}
          size={17}
          strokeWidth={1.5}
        />
      </button>
      <button
        aria-label={t("projectEdit.unlinkWorkspace", { path: workspacePathLabel })}
        className="grid h-9 w-9 place-items-center rounded-full border border-[var(--color-outline)] bg-transparent text-[var(--color-on-island)] transition-colors duration-[var(--motion-fast)] disabled:cursor-not-allowed disabled:opacity-55"
        disabled={disabled}
        onClick={onUnlink}
        type="button"
      >
        <Link2Off aria-hidden="true" size={18} strokeWidth={1.5} />
      </button>
    </article>
  );
}

export function WorkspaceUnlinkDialog({
  disabled,
  onClose,
  onConfirm,
  target,
}: Readonly<{
  disabled: boolean;
  onClose: () => void;
  onConfirm: (target: WorkspaceUnlinkTarget) => void;
  target: WorkspaceUnlinkTarget;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      className="w-[min(var(--workspace-unlink-dialog-width),calc(100vw-32px))]"
      closeLabel={t("app.close")}
      onClose={onClose}
      open
      style={workspaceUnlinkDialogStyle}
      title={t("projectEdit.unlinkTitle")}
    >
      <WorkspaceUnlinkContent
        disabled={disabled}
        onCancel={onClose}
        onConfirm={() => {
          onConfirm(target);
        }}
        rootPath={target.rootPath}
      />
    </Dialog>
  );
}

function WorkspaceUnlinkContent({
  className,
  disabled,
  onCancel,
  onConfirm,
  rootPath,
  style,
}: Readonly<{
  className?: string;
  disabled: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  rootPath: string;
  style?: CSSProperties;
}>) {
  const { t } = useTranslation();
  const { homePath, nativeBridge } = useAppServices();
  const rootPathLabel = formatHomeRelativePath(rootPath, homePath, nativeBridge.capabilities.platform);
  return (
    <div className={cx("grid gap-[var(--space-3)]", className)} style={style}>
      <p className="m-0">{t("projectEdit.unlinkBody")}</p>
      <p
        className="m-0 break-words rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] font-mono text-sm"
        title={rootPath}
      >
        {rootPathLabel}
      </p>
      <div className="flex flex-wrap justify-end gap-[var(--space-2)]">
        <Button disabled={disabled} onClick={onCancel} variant="secondary">
          {t("app.cancel")}
        </Button>
        <Button disabled={disabled} onClick={onConfirm} variant="danger">
          <span className="inline-flex items-center gap-[var(--space-2)]">
            <Unlink aria-hidden="true" size={16} strokeWidth={1.5} />
            {t("projectEdit.unlinkConfirm")}
          </span>
        </Button>
      </div>
    </div>
  );
}
