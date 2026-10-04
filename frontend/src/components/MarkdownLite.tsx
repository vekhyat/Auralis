import { Fragment, type ReactNode } from "react";
import { openExternal } from "@/lib/utils";

function renderInline(text: string, keyPrefix: string): ReactNode[] {
    const nodes: ReactNode[] = [];
    const pattern = /\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let i = 0;
    while ((match = pattern.exec(text)) !== null) {
        if (match.index > lastIndex) {
            nodes.push(<Fragment key={`${keyPrefix}-t${i}`}>{text.slice(lastIndex, match.index)}</Fragment>);
        }
        if (match[1] !== undefined && match[2] !== undefined) {
            const label = match[1];
            const url = match[2];
            nodes.push(<button key={`${keyPrefix}-l${i}`} type="button" onClick={() => openExternal(url)} className="text-primary underline hover:opacity-80 bg-transparent border-none p-0 cursor-pointer">
                    {label}
                </button>);
        }
        else if (match[3] !== undefined) {
            nodes.push(<strong key={`${keyPrefix}-b${i}`} className="font-semibold text-foreground">{match[3]}</strong>);
        }
        else if (match[4] !== undefined) {
            nodes.push(<em key={`${keyPrefix}-i${i}`}>{match[4]}</em>);
        }
        else if (match[5] !== undefined) {
            nodes.push(<code key={`${keyPrefix}-c${i}`} className="rounded bg-muted px-1 py-0.5 font-mono text-xs">{match[5]}</code>);
        }
        lastIndex = pattern.lastIndex;
        i++;
    }
    if (lastIndex < text.length) {
        nodes.push(<Fragment key={`${keyPrefix}-t${i}`}>{text.slice(lastIndex)}</Fragment>);
    }
    return nodes;
}
export function MarkdownLite({ content }: {
    content: string;
}) {
    const lines = (content || "").replace(/\r\n/g, "\n").split("\n");
    const blocks: ReactNode[] = [];
    let listItems: string[] = [];
    let key = 0;
    const flushList = () => {
        if (listItems.length === 0)
            return;
        const items = listItems;
        listItems = [];
        blocks.push(<ul key={`ul-${key++}`} className="list-disc space-y-1 pl-5">
                {items.map((item, idx) => (<li key={idx}>{renderInline(item, `li-${key}-${idx}`)}</li>))}
            </ul>);
    };
    for (const raw of lines) {
        const line = raw.trimEnd();
        const bullet = line.match(/^\s*[-*]\s+(.*)$/);
        if (bullet) {
            listItems.push(bullet[1]);
            continue;
        }
        flushList();
        const heading = line.match(/^(#{1,6})\s+(.*)$/);
        if (heading) {
            blocks.push(<p key={`h-${key++}`} className="font-semibold text-foreground">
                    {renderInline(heading[2], `h-${key}`)}
                </p>);
            continue;
        }
        if (line.trim() === "") {
            continue;
        }
        blocks.push(<p key={`p-${key++}`}>{renderInline(line, `p-${key}`)}</p>);
    }
    flushList();
    return <div className="space-y-2 text-sm text-muted-foreground">{blocks}</div>;
}
