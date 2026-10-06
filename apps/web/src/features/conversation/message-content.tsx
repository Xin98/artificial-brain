import { Fragment } from "react";

// Render a deliberately small Markdown subset as React nodes. Raw HTML is
// always text; links accept only explicit web URLs or same-origin paths.
function inline(text: string): React.ReactNode[] {
  const tokens = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g);
  return tokens.map((token, index) => {
    if (token.startsWith("**") && token.endsWith("**"))
      return <strong key={index}>{token.slice(2, -2)}</strong>;
    if (token.startsWith("`") && token.endsWith("`"))
      return <code key={index}>{token.slice(1, -1)}</code>;
    const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token);
    if (link) {
      const href = link[2];
      if (
        /^https?:\/\//i.test(href) ||
        (/^\/(?!\/)/.test(href) && !/[\\\u0000-\u0020]/.test(href))
      )
        return (
          <a key={index} href={href} rel="noopener noreferrer">
            {link[1]}
          </a>
        );
      return <Fragment key={index}>{link[1]}</Fragment>;
    }
    return <Fragment key={index}>{token}</Fragment>;
  });
}

export function MessageContent({ text }: { text: string }): React.JSX.Element {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const blocks: React.ReactNode[] = [];
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (line.startsWith("```")) {
      const code: string[] = [];
      while (++index < lines.length && !lines[index].startsWith("```"))
        code.push(lines[index]);
      blocks.push(
        <pre key={index}>
          <code>{code.join("\n")}</code>
        </pre>,
      );
    } else if (/^#{1,6}\s/.test(line)) {
      blocks.push(
        <h3 key={index}>{inline(line.replace(/^#{1,6}\s+/, ""))}</h3>,
      );
    } else if (/^\s*(?:[-*]|\d+\.)\s/.test(line)) {
      const ordered = /^\s*\d+\./.test(line);
      const items: React.ReactNode[] = [];
      const pattern = ordered ? /^\s*\d+\.\s/ : /^\s*[-*]\s/;
      do {
        items.push(
          <li key={index}>{inline(lines[index].replace(pattern, ""))}</li>,
        );
        index++;
      } while (index < lines.length && pattern.test(lines[index]));
      index--;
      blocks.push(
        ordered ? <ol key={index}>{items}</ol> : <ul key={index}>{items}</ul>,
      );
    } else if (line.trim()) {
      const paragraph = [line];
      while (
        index + 1 < lines.length &&
        lines[index + 1].trim() &&
        !/^(?:```|#{1,6}\s|\s*(?:[-*]|\d+\.)\s)/.test(lines[index + 1])
      )
        paragraph.push(lines[++index]);
      blocks.push(<p key={index}>{inline(paragraph.join("\n"))}</p>);
    }
  }
  return <div className="message-content">{blocks}</div>;
}
