import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { safeExternalUrl } from "./externalLinks";

export type MarkdownTextProps = Readonly<{
  value: string;
  onOpenLink?: (url: string) => void;
}>;

export function MarkdownText({ value, onOpenLink }: MarkdownTextProps) {
  return (
    <ReactMarkdown
      components={markdownComponents(onOpenLink)}
      rehypePlugins={[rehypeSanitize]}
      remarkPlugins={[remarkGfm]}
      skipHtml
    >
      {value}
    </ReactMarkdown>
  );
}

function markdownComponents(onOpenLink: MarkdownTextProps["onOpenLink"]): Components {
  return {
    a({ children, href }) {
      const safeHref = safeExternalUrl(href);
      if (safeHref === undefined) {
        return <span>{children}</span>;
      }
      return (
        <a
          href={safeHref}
          onClick={(event) => {
            if (onOpenLink === undefined) {
              return;
            }
            event.preventDefault();
            onOpenLink(safeHref);
          }}
          rel="noreferrer"
          target="_blank"
        >
          {children}
        </a>
      );
    },
    code({ children }) {
      return <code>{children}</code>;
    },
    pre({ children }) {
      return <pre>{children}</pre>;
    },
  };
}
