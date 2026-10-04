export function extractMarkdownSection(body: string, heading: string): string {
    const text = (body || "").replace(/\r\n/g, "\n");
    const lines = text.split("\n");
    const target = heading.trim().toLowerCase();
    let start = -1;
    for (let i = 0; i < lines.length; i++) {
        const m = lines[i].match(/^#{1,6}\s+(.*)$/);
        if (m && m[1].trim().toLowerCase() === target) {
            start = i + 1;
            break;
        }
    }
    if (start === -1) {
        return text.trim();
    }
    const collected: string[] = [];
    for (let i = start; i < lines.length; i++) {
        if (/^#{1,6}\s+/.test(lines[i])) {
            break;
        }
        collected.push(lines[i]);
    }
    return collected.join("\n").trim();
}
