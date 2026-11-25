import { useMemo, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as AsyncResult from "effect/reactivity/AsyncResult";
import * as Effect from "effect/Effect";
import { useInView } from "motion/react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { useAppServices, type AppServices } from "@/app-facade";
import { Spinner, showStatusToast } from "@/ui";
import { StaticMarkdown, type MarkdownLocalFiles } from "@/ui/markdown";
import { openOutputFile, type OutputFileOpenTarget } from "./outputFileActions";
import type { WorkflowOutputRoot } from "./outputRoot";

type OutputFile = Awaited<ReturnType<AppServices["nativeBridge"]["files"]["loadOutputFile"]>>;

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
            fileUrl={target.fileUrl}
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

type LocalFileProps = Readonly<{
  children: ReactNode;
  literal: boolean;
  path: string;
  fileUrl?: string | undefined;
  root: WorkflowOutputRoot;
}>;

function OutputLocalFile(props: LocalFileProps) {
  const { nativeBridge } = useAppServices();
  if (!nativeBridge.capabilities.files.stat) {
    return <>{props.literal ? props.children : <OutputFileAction path={props.path} target={undefined} />}</>;
  }
  return <VisibleOutputFile {...props} />;
}

function VisibleOutputFile(props: LocalFileProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const visible = useInView(ref);
  return (
    <span ref={ref} className="inline-grid min-w-0 max-w-full">
      {visible ? <LoadedOutputFile {...props} /> : props.children}
    </span>
  );
}

function LoadedOutputFile({ children, literal, path, fileUrl, root }: LocalFileProps) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const file = useMemo(
    () =>
      Atom.make(
        Effect.acquireRelease(
          Effect.tryPromise({
            try: async () =>
              nativeBridge.files.loadOutputFile(
                fileUrl !== undefined
                  ? { fileUrl }
                  : root.kind === "unavailable"
                    ? { path }
                    : { path, basePath: root.path },
              ),
            catch: (cause) => new OutputFileFailure(cause),
          }),
          (result) =>
            Effect.sync(() => {
              if (result.kind === "image") result.release();
            }),
        ).pipe(
          Effect.catch((cause) =>
            Effect.sync(() => {
              showStatusToast({
                id: "workflow-output-file-load-failed",
                title: t("app.filePreviewFailed"),
                body: errorMessage(cause),
                tone: "danger",
              });
              return { kind: "failed" } as const;
            }),
          ),
        ),
      ),
    [nativeBridge, path, fileUrl, root, t],
  );
  const state = useAtomValue(file);
  if (!AsyncResult.isSuccess(state))
    return (
      <span>
        {children} <Spinner size="sm" />
      </span>
    );
  const result = state.value;
  if (result.kind === "missing") return <>{children}</>;
  if (result.kind === "unavailable")
    return <>{literal ? children : <OutputFileAction path={path} target={undefined} />}</>;
  if (result.kind === "failed") return <OutputFileAction path={path} target={undefined} />;
  if (result.kind === "image") return <OutputImage file={result} path={path} />;
  return <OutputFileAction path={path} target={result.target} />;
}

function OutputImage({
  file,
  path,
}: Readonly<{ file: Extract<OutputFile, { kind: "image" }>; path: string }>) {
  const [decoder, setDecoder] = useState<"loading" | "loaded" | "failed">("loading");
  if (decoder === "failed") return <OutputFileAction path={path} target={file.target} />;
  return (
    <span className="inline-grid min-w-0 max-w-full gap-[var(--space-1)]">
      {decoder === "loading" ? <Spinner size="sm" /> : null}
      <img
        src={file.url}
        alt={path}
        className="max-h-[60vh] max-w-full object-contain"
        onLoad={() => {
          setDecoder("loaded");
        }}
        onError={() => {
          setDecoder("failed");
        }}
      />
      <OutputFileAction path={path} target={file.target} />
    </span>
  );
}

function OutputFileAction({
  path,
  target,
}: Readonly<{ path: string; target: OutputFileOpenTarget | undefined }>) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const action = useMemo(
    () => Atom.fn(() => Effect.promise(async () => openOutputFile(path, target, nativeBridge, t))),
    [path, target, nativeBridge, t],
  );
  const open = useAtomSet(action);
  const state = useAtomValue(action);
  function activate(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    event.stopPropagation();
    if (event.button <= 1) open();
  }
  return (
    <button
      type="button"
      disabled={state.waiting}
      className="max-w-full break-all text-left font-mono text-[var(--color-primary)] underline-offset-2 hover:underline"
      onClick={activate}
      onAuxClick={activate}
    >
      {path} {state.waiting ? <Spinner size="sm" /> : null}
    </button>
  );
}
