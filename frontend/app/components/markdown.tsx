import ReactMarkdown from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";

import { cn } from "~/lib/utils";

/**
 * Renders user-authored Markdown. Raw HTML is never interpreted and unsafe
 * link schemes are dropped by react-markdown's default URL transform.
 */
export function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn("markdown", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={{
          a: ({ node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer nofollow" />,
          img: ({ node, ...props }) => <img {...props} loading="lazy" referrerPolicy="no-referrer" />,
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
