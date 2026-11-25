import type { Plugin } from "unified";
import type rehypeSanitize from "rehype-sanitize";
import type remarkParse from "remark-parse";
import type { ReactNode } from "react";

import { markdownLinkTarget } from "./markdownLinkTarget";

type MarkdownRoot =
  typeof remarkParse extends Plugin<Parameters<typeof remarkParse>, string, infer Root> ? Root : never;
type MarkdownNode = MarkdownRoot | MarkdownRoot["children"][number];
type HtmlRoot = Parameters<ReturnType<typeof rehypeSanitize>>[0];

export type MarkdownLocalFile = Extract<ReturnType<typeof markdownLinkTarget>, { kind: "file" }>;
export type MarkdownLocalFiles = Readonly<{
  render(target: MarkdownLocalFile, fallback: ReactNode, source: "literal" | "destination"): ReactNode;
}>;
interface MarkdownLocalFileState {
  destinations: Map<number, MarkdownLocalFile>;
  whole: MarkdownLocalFile | undefined;
}
type LocalDestination = Readonly<{
  target: MarkdownLocalFile;
  source: "literal" | "destination";
}>;

// Streamdown shares processors by plugin name. Keep plugins stateless and attach
// parse-local metadata to their library-owned document/node identities instead.
const filesByDocument = new WeakMap<object, MarkdownLocalFileState>();
const filesByNode = new WeakMap<object, LocalDestination>();

export function markdownLocalFileForNode(node: object | undefined): LocalDestination | undefined {
  return node === undefined ? undefined : filesByNode.get(node);
}

function wholeValueFileTarget(value: string, root: MarkdownRoot): MarkdownLocalFile | undefined {
  if (wholePlainText(root) !== value || value === "") return undefined;
  if (URL.canParse(value)) {
    const target = markdownLinkTarget(value);
    if (target !== undefined) return target.kind === "file" ? target : undefined;
  }
  return { kind: "file", path: value };
}

function wholePlainText(root: MarkdownRoot): string | undefined {
  if (root.children.length !== 1) return undefined;
  const paragraph = root.children[0];
  if (paragraph?.type !== "paragraph" || paragraph.children.length !== 1) return undefined;
  const text = paragraph.children[0];
  return text?.type === "text" ? text.value : undefined;
}

// Preserve parsed Markdown destinations before HTML sanitization removes local image sources.
// The renderer never passes these paths to a browser image or navigation API.
export const markdownLocalFilesRemark: Plugin<[], MarkdownRoot> = () => {
  return (root, file) => {
    const state: MarkdownLocalFileState = { destinations: new Map(), whole: undefined };
    const definitions = new Map<string, string>();
    function collect(node: MarkdownNode): void {
      if (node.type === "definition") definitions.set(node.identifier, node.url);
      if ("children" in node) for (const child of node.children) collect(child);
    }
    collect(root);
    function annotate(node: MarkdownNode): void {
      const destination =
        node.type === "link" || node.type === "image"
          ? node.url
          : node.type === "linkReference" || node.type === "imageReference"
            ? definitions.get(node.identifier)
            : undefined;
      const target = markdownLinkTarget(destination, true);
      const offset = node.position?.start.offset;
      if (target?.kind === "file" && offset !== undefined) {
        state.destinations.set(offset, target);
      }
      if ("children" in node) for (const child of node.children) annotate(child);
    }
    annotate(root);
    const whole = wholeValueFileTarget(String(file), root);
    state.whole = whole;
    filesByDocument.set(file, state);
  };
};

export const markdownLocalFilesRehype: Plugin<[], HtmlRoot> = () => {
  return (root, file) => {
    const state = filesByDocument.get(file);
    if (state === undefined) return;
    function attach(node: HtmlRoot | HtmlRoot["children"][number], context: MarkdownLocalFileState): void {
      if (node.type === "element") {
        const selected = localElementDestination(node, context);
        if (selected !== undefined) filesByNode.set(node, selected);
      }
      if ("children" in node) for (const child of node.children) attach(child, context);
    }
    attach(root, state);
  };
};

function localElementDestination(
  node: Extract<HtmlRoot["children"][number], { type: "element" }>,
  state: MarkdownLocalFileState,
): LocalDestination | undefined {
  if (node.tagName === "p" && state.whole !== undefined) {
    return { target: state.whole, source: "literal" };
  }
  if (node.tagName !== "a" && node.tagName !== "img") return undefined;
  const offset = node.position?.start.offset;
  const destination = offset === undefined ? undefined : state.destinations.get(offset);
  return destination === undefined ? undefined : { target: destination, source: "destination" };
}
