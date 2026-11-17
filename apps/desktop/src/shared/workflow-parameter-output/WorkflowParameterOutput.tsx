import { useMemo, useRef, useState, type ReactNode } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as AsyncResult from "effect/reactivity/AsyncResult";
import * as Effect from "effect/Effect";
import { useInView } from "motion/react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { useAppServices } from "@/app-facade";
import { Spinner, showStatusToast } from "@/ui";
import { StaticMarkdown, type MarkdownLocalFiles } from "@/ui/markdown";
import { openOutputFile } from "./outputFileActions";
import { outputFileTarget, type WorkflowOutputRoot } from "./outputFileTarget";

class OutputFileFailure extends Error {
  readonly _tag = "OutputFileFailure";
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
  }
}

export function WorkflowParameterOutput({
  value,
  root,
}: Readonly<{ value: string; root: WorkflowOutputRoot }>) {
  const localFiles = useMemo(
    () =>
      ({
        render: (target, fallback, source) => (
          <OutputLocalFile
            key={JSON.stringify([target.path, root])}
            path={target.path}
            root={root}
            literal={source === "literal"}
          >
            {fallback}
          </OutputLocalFile>
        ),
      }) satisfies MarkdownLocalFiles,
    [root],
  );
  return <StaticMarkdown key={value} value={value} localFiles={localFiles} />;
}

function OutputLocalFile({
  children,
  literal,
  path,
  root,
}: Readonly<{
  children: ReactNode;
  literal: boolean;
  path: string;
  root: WorkflowOutputRoot;
}>) {
  const { nativeBridge } = useAppServices();
  const resolved = outputFileTarget(path, root);
  if (!nativeBridge.capabilities.files.stat || resolved.kind === "unavailable") {
    return <>{literal ? children : <OutputFileAction path={path} root={root} />}</>;
  }
  return (
    <VisibleOutputFile path={path} root={root}>
      {children}
    </VisibleOutputFile>
  );
}

function VisibleOutputFile({
  children,
  path,
  root,
}: Readonly<{ children: ReactNode; path: string; root: WorkflowOutputRoot }>) {
  const ref = useRef<HTMLSpanElement>(null);
  const visible = useInView(ref);
  return (
    <span ref={ref} className="inline-grid min-w-0 max-w-full">
      {visible ? (
        <InspectedOutputFile path={path} root={root}>
          {children}
        </InspectedOutputFile>
      ) : (
        children
      )}
    </span>
  );
}

function InspectedOutputFile({
  children,
  path,
  root,
}: Readonly<{ children: ReactNode; path: string; root: WorkflowOutputRoot }>) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const [unsupported, setUnsupported] = useState(false);
  const inspection = useMemo(
    () =>
      Atom.make(
        Effect.tryPromise({
          try: async () => {
            const resolved = outputFileTarget(path, root);
            if (resolved.kind === "unavailable") return { kind: "unavailable" } as const;
            return nativeBridge.files.inspectOutputFile(resolved.target);
          },
          catch: (cause) => new OutputFileFailure(cause),
        }).pipe(
          Effect.catch((cause) =>
            Effect.sync(() => {
              showStatusToast({
                id: "workflow-output-file-inspection-failed",
                title: t("app.filePreviewFailed"),
                body: errorMessage(cause),
                tone: "danger",
              });
              return { kind: "preview_ineligible" } as const;
            }),
          ),
        ),
      ),
    [nativeBridge, path, root, t],
  );
  const state = useAtomValue(inspection);
  if (!AsyncResult.isSuccess(state))
    return (
      <span>
        {children} <Spinner size="sm" />
      </span>
    );
  const result = state.value;
  if (result.kind === "missing" || result.kind === "unavailable") return <>{children}</>;
  if (result.kind !== "image" || unsupported) return <OutputFileAction path={path} root={root} />;
  return (
    <OutputImagePreview
      path={path}
      root={root}
      onUnsupported={() => {
        setUnsupported(true);
      }}
    />
  );
}

function OutputImagePreview({
  path,
  root,
  onUnsupported,
}: Readonly<{
  path: string;
  root: WorkflowOutputRoot;
  onUnsupported(): void;
}>) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const [loaded, setLoaded] = useState(false);
  const preview = useMemo(
    () =>
      Atom.make(
        Effect.acquireRelease(
          Effect.tryPromise({
            try: async () => {
              const resolved = outputFileTarget(path, root);
              if (resolved.kind === "unavailable") return { kind: "unavailable" } as const;
              return nativeBridge.files.readOutputImage(resolved.target);
            },
            catch: (cause) => new OutputFileFailure(cause),
          }),
          (image) =>
            Effect.sync(() => {
              if (image.kind === "image") image.release();
            }),
        ).pipe(
          Effect.catch((cause) =>
            Effect.sync(() => {
              showStatusToast({
                id: "workflow-output-file-preview-failed",
                title: t("app.filePreviewFailed"),
                body: errorMessage(cause),
                tone: "danger",
              });
              return { kind: "preview_ineligible" } as const;
            }),
          ),
        ),
      ),
    [nativeBridge, path, root, t],
  );
  const state = useAtomValue(preview);
  if (!AsyncResult.isSuccess(state))
    return (
      <span>
        <OutputFileAction path={path} root={root} /> <Spinner size="sm" />
      </span>
    );
  const image = state.value;
  if (image.kind !== "image") return <OutputFileAction path={path} root={root} />;
  return (
    <span className="inline-grid min-w-0 max-w-full gap-[var(--space-1)]">
      {!loaded ? <Spinner size="sm" /> : null}
      <img
        src={image.url}
        alt={path}
        className="max-h-[60vh] max-w-full object-contain"
        onLoad={() => {
          setLoaded(true);
        }}
        onError={onUnsupported}
      />
      <OutputFileAction path={path} root={root} />
    </span>
  );
}

function OutputFileAction({ path, root }: Readonly<{ path: string; root: WorkflowOutputRoot }>) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const action = useMemo(
    () => Atom.fn(() => Effect.promise(async () => openOutputFile(path, root, nativeBridge, t))),
    [path, root, nativeBridge, t],
  );
  const open = useAtomSet(action);
  const state = useAtomValue(action);
  return (
    <button
      type="button"
      disabled={state.waiting}
      className="max-w-full break-all text-left font-mono text-[var(--color-primary)] underline-offset-2 hover:underline"
      onClick={() => {
        open();
      }}
    >
      {path} {state.waiting ? <Spinner size="sm" /> : null}
    </button>
  );
}
