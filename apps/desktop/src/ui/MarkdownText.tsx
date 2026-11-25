import { createContext, useContext, useMemo, type ComponentProps, type MouseEvent } from "react";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import rehypeSanitize, { type Options as SanitizeOptions } from "rehype-sanitize";
import {
  Streamdown,
  defaultRehypePlugins,
  defaultUrlTransform,
  defaultComponents,
  type AnimateOptions,
  type Components,
  type CustomRendererProps,
  type ExtraProps,
  type PluginConfig,
} from "streamdown";

import { Checkbox } from "./radix/checkbox";
import { markdownLinkTarget } from "./markdownLinkTarget";
import { syntaxHighlightingLanguageHints } from "./syntaxHighlighting";
import { SyntaxHighlightedCode } from "./SyntaxHighlightedCode";
import { projectMarkdownText } from "./taskBodyMarkdownText";
import { MarkdownLinkContext } from "./MarkdownLinkContext";
import {
  markdownLocalFilesRemark,
  markdownLocalFilesRehype,
  markdownLocalFileForNode,
  type MarkdownLocalFiles,
} from "./markdownLocalFiles";
import "streamdown/styles.css";
import "./MarkdownText.css";
import "./StreamdownMarkdown.css";

// @streamdown/code@1.1.1 has a lossy unbounded token-result cache; Chat can
// re-evaluate a newer official highlighter when it adopts this seam.
export type StaticMarkdownProps = Readonly<{
  disabled?: boolean;
  onTaskListChange?: (value: string) => void;
  taskListItemToggleLabel?: (checked: boolean) => string;
  value: string;
  localFiles?: MarkdownLocalFiles;
}>;
export type StreamingMarkdownProps = Readonly<{ value: string; streaming?: boolean }>;
export type TaskBodyMarkdownProps = Readonly<{ value: string }>;

type MarkdownTaskListItemContextValue = Readonly<{
  onChange: StaticMarkdownProps["onTaskListChange"];
  sourceOffset: number | undefined;
  taskListItemToggleLabel: StaticMarkdownProps["taskListItemToggleLabel"];
  value: string;
  disabled: boolean;
}>;

const MarkdownTaskListItemContext = createContext<MarkdownTaskListItemContextValue | null>(null);
const LocalFilesContext = createContext<MarkdownLocalFiles | null>(null);
const languageHints = syntaxHighlightingLanguageHints();
const linkSafety = { enabled: false };
const markdownPlugins = {
  renderers: [{ component: MarkdownHighlightedCode, language: languageHints }],
} satisfies PluginConfig;
const markdownRemarkPlugins = [[remarkGfm, {}], remarkBreaks] satisfies NonNullable<
  ComponentProps<typeof Streamdown>["remarkPlugins"]
>;
const streamdownSanitize = defaultRehypePlugins.sanitize;
const streamdownRaw = defaultRehypePlugins.raw;
if (streamdownRaw === undefined || !Array.isArray(streamdownSanitize)) {
  throw new Error("Streamdown must supply its raw HTML and sanitizer configuration.");
}
const streamdownSanitizeParameters: readonly unknown[] = streamdownSanitize.slice(1);
// Harden unconditionally blocks file: URLs. Keep the HTML/image sanitizer;
// MarkdownExternalLink validates every anchor before making it interactive.
const markdownRehypePlugins = [
  streamdownRaw,
  [markdownFileSanitize, ...streamdownSanitizeParameters],
] satisfies NonNullable<ComponentProps<typeof Streamdown>["rehypePlugins"]>;
const outputRemarkPlugins = [...markdownRemarkPlugins, markdownLocalFilesRemark];
const outputRehypePlugins = [...markdownRehypePlugins, markdownLocalFilesRehype];

function markdownFileSanitize(options: SanitizeOptions) {
  return rehypeSanitize({
    ...options,
    protocols: {
      ...options.protocols,
      href: [...(options.protocols?.href ?? []), "file"],
    },
  });
}
const markdownAnimation = {
  animation: "blurIn",
  duration: 50,
  easing: "ease",
  sep: "char",
  stagger: 13,
} satisfies AnimateOptions;
const richComponents = {
  a: MarkdownExternalLink,
  input: MarkdownTaskListCheckbox,
  li: MarkdownTaskListItem,
  p: "div",
  img: MarkdownImage,
} satisfies Pick<Components, "a" | "input" | "li" | "p" | "img">;
const outputComponents = { ...richComponents, p: MarkdownParagraph } satisfies Components;

export function StaticMarkdown({
  disabled = false,
  onTaskListChange,
  taskListItemToggleLabel,
  value,
  localFiles,
}: StaticMarkdownProps) {
  return (
    <MarkdownCore
      animated={false}
      disabled={disabled}
      onChange={onTaskListChange}
      taskListItemToggleLabel={taskListItemToggleLabel}
      value={value}
      localFiles={localFiles}
    />
  );
}

export function StreamingMarkdown({ value, streaming = true }: StreamingMarkdownProps) {
  return <MarkdownCore animated={streaming} value={value} />;
}

export function TaskBodyMarkdown({ value }: TaskBodyMarkdownProps) {
  return <span className="markdown-plain-text">{projectMarkdownText(value)}</span>;
}

function MarkdownCore({
  animated,
  disabled = false,
  onChange,
  taskListItemToggleLabel,
  value,
  localFiles,
}: Readonly<{
  animated: boolean;
  disabled?: boolean;
  onChange?: StaticMarkdownProps["onTaskListChange"];
  taskListItemToggleLabel?: StaticMarkdownProps["taskListItemToggleLabel"];
  value: string;
  localFiles?: MarkdownLocalFiles | undefined;
}>) {
  const interaction = useMemo(
    () => ({ disabled, onChange, taskListItemToggleLabel, value, sourceOffset: undefined }),
    [disabled, onChange, taskListItemToggleLabel, value],
  );
  return (
    <MarkdownTaskListItemContext.Provider value={interaction}>
      <LocalFilesContext.Provider value={localFiles ?? null}>
        <Streamdown
          animated={animated ? markdownAnimation : false}
          className={`markdown-text${animated ? " markdown-text-streaming" : ""}`}
          components={localFiles === undefined ? richComponents : outputComponents}
          controls={false}
          isAnimating={animated}
          linkSafety={linkSafety}
          mode={animated ? "streaming" : "static"}
          plugins={markdownPlugins}
          remarkPlugins={localFiles === undefined ? markdownRemarkPlugins : outputRemarkPlugins}
          rehypePlugins={localFiles === undefined ? markdownRehypePlugins : outputRehypePlugins}
          urlTransform={markdownUrlTransform}
          {...(onChange === undefined && localFiles === undefined
            ? {}
            : { parseMarkdownIntoBlocksFn: singleMarkdownBlock })}
        >
          {value}
        </Streamdown>
      </LocalFilesContext.Provider>
    </MarkdownTaskListItemContext.Provider>
  );
}

const markdownUrlTransform: NonNullable<ComponentProps<typeof Streamdown>["urlTransform"]> = (
  url,
  key,
  node,
) => {
  if (node.tagName === "a" && key === "href" && markdownLinkTarget(url) !== undefined) return url;
  return defaultUrlTransform(url, key, node);
};

function MarkdownExternalLink({ children, href, className, title, node }: ComponentProps<"a"> & ExtraProps) {
  const actions = useContext(MarkdownLinkContext);
  const local = useContext(LocalFilesContext);
  const destination = markdownLocalFileForNode(node);
  if (destination !== undefined && local !== null)
    return <>{local.render(destination.target, children, destination.source)}</>;
  const target = markdownLinkTarget(href);
  if (target === undefined) return <span>{children}</span>;
  if (actions === null) throw new Error("MarkdownLinkProvider is required for links.");
  if (target.kind === "file" && actions.openFile === undefined) return <span>{children}</span>;
  const url = target.kind === "external" ? target.url : href;
  const openLink = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button > 1) return;
    event.preventDefault();
    if (target.kind === "external") actions.openExternal(target.url);
    else actions.openFile?.(target.path);
  };
  return (
    <a
      className={className}
      href={url}
      title={title}
      rel="noopener noreferrer"
      target="_blank"
      onClick={openLink}
      onAuxClick={openLink}
    >
      {children}
    </a>
  );
}

function MarkdownParagraph({ children, node }: ComponentProps<"p"> & ExtraProps) {
  const local = useContext(LocalFilesContext);
  const destination = markdownLocalFileForNode(node);
  return (
    <div>
      {destination === undefined || local === null
        ? children
        : local.render(destination.target, children, destination.source)}
    </div>
  );
}

function MarkdownImage(props: ComponentProps<"img"> & ExtraProps) {
  const local = useContext(LocalFilesContext);
  const destination = markdownLocalFileForNode(props.node);
  if (destination !== undefined && local !== null)
    return <>{local.render(destination.target, props.alt, "destination")}</>;
  return <defaultComponents.img {...props} />;
}

function MarkdownTaskListItem({ children, node, ...props }: ComponentProps<"li"> & ExtraProps) {
  const taskListItem = useContext(MarkdownTaskListItemContext);
  const className = node?.properties.className;
  const isTaskItem = className?.some((value) => value === "task-list-item") === true;
  if (!isTaskItem) return <li {...props}>{children}</li>;
  if (taskListItem === null) return <li {...props}>{children}</li>;
  return (
    <MarkdownTaskListItemContext.Provider
      value={{ ...taskListItem, sourceOffset: node?.position?.start.offset }}
    >
      <li {...props}>{children}</li>
    </MarkdownTaskListItemContext.Provider>
  );
}

function MarkdownTaskListCheckbox({ checked, type }: ComponentProps<"input"> & ExtraProps) {
  const checkedValue = checked === true;
  const taskListItem = useContext(MarkdownTaskListItemContext);
  if (type !== "checkbox") return null;
  const sourceOffset = taskListItem?.sourceOffset;
  const editable = taskListItem?.onChange !== undefined && sourceOffset !== undefined;
  const checkedFromSource = editable ? taskListChecked(taskListItem.value, sourceOffset) : checkedValue;
  return (
    <Checkbox
      checked={checkedFromSource}
      className="markdown-task-list-checkbox"
      disabled={taskListItem?.disabled !== false || !editable}
      onCheckedChange={(nextChecked) => {
        if (nextChecked === "indeterminate" || !editable) return;
        taskListItem.onChange(toggleTaskListMarker(taskListItem.value, sourceOffset, nextChecked));
      }}
      {...(taskListItem?.taskListItemToggleLabel === undefined
        ? {}
        : { "aria-label": taskListItem.taskListItemToggleLabel(checkedFromSource) })}
    />
  );
}

function taskListChecked(value: string, sourceOffset: number): boolean {
  const markerOffset = taskListMarkerOffset(value, sourceOffset);
  return value[markerOffset] === "x" || value[markerOffset] === "X";
}

function MarkdownHighlightedCode({ code, language, isIncomplete }: CustomRendererProps) {
  return <SyntaxHighlightedCode code={code} languageHint={isIncomplete ? undefined : language} />;
}

function singleMarkdownBlock(markdown: string): string[] {
  return [markdown];
}

function toggleTaskListMarker(value: string, sourceOffset: number, checked: boolean): string {
  const markerOffset = taskListMarkerOffset(value, sourceOffset);
  return `${value.slice(0, markerOffset)}${checked ? "x" : " "}${value.slice(markerOffset + 1)}`;
}

function taskListMarkerOffset(value: string, sourceOffset: number): number {
  let cursor = listMarkerEndOffset(value, sourceOffset);
  if (!isHorizontalWhitespace(value[cursor])) {
    throw invalidTaskListMarker(value, sourceOffset);
  }
  while (isHorizontalWhitespace(value[cursor])) {
    cursor += 1;
  }
  return taskListStateOffset(value, sourceOffset, cursor);
}

function listMarkerEndOffset(value: string, sourceOffset: number): number {
  const first = value[sourceOffset];
  if (first === "-" || first === "+" || first === "*") {
    return sourceOffset + 1;
  }
  return orderedListMarkerEndOffset(value, sourceOffset);
}

function orderedListMarkerEndOffset(value: string, sourceOffset: number): number {
  let cursor = sourceOffset;
  const first = value[cursor];
  if (!isAsciiDigit(first)) {
    throw invalidTaskListMarker(value, sourceOffset);
  }
  let digitCount = 0;
  while (isAsciiDigit(value[cursor]) && digitCount < 9) {
    cursor += 1;
    digitCount += 1;
  }
  if (value[cursor] !== "." && value[cursor] !== ")") {
    throw invalidTaskListMarker(value, sourceOffset);
  }
  return cursor + 1;
}

function taskListStateOffset(value: string, sourceOffset: number, checkboxOffset: number): number {
  const stateOffset = checkboxOffset + 1;
  const state = value[stateOffset];
  if (
    value[checkboxOffset] !== "[" ||
    (state !== " " && state !== "x" && state !== "X") ||
    value[checkboxOffset + 2] !== "]"
  ) {
    throw invalidTaskListMarker(value, sourceOffset);
  }
  return stateOffset;
}

function isAsciiDigit(value: string | undefined): boolean {
  return value !== undefined && value >= "0" && value <= "9";
}

function isHorizontalWhitespace(value: string | undefined): boolean {
  return value === " " || value === "\t";
}

function invalidTaskListMarker(value: string, sourceOffset: number): Error {
  return new Error(
    `Markdown task-list invariant failed at source offset ${String(sourceOffset)} in ${String(value.length)} characters.`,
  );
}
