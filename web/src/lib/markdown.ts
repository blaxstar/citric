import { CLIPBOARD_SVG } from '../components/icons'
/**
 * Lightweight markdown renderer. Zero external dependencies, sanitized output.
 * Supports headings, paragraphs, blockquotes, lists,
 * fenced code blocks, inline styles, links, and admonitions.
 */

const HEADING_PATTERN = /^(#{1,6})\s+(.*)$/;
const LIST_PATTERN = /^[-*]\s+/;
const QUOTE_PATTERN = /^>\s?/;
const CODE_FENCE_PATTERN = /^```/;
const ADMONITION_OPEN_PATTERN = /^:::([\w][\w\s.\-]*?)\s*$/;
const ADMONITION_CLOSE_PATTERN = /^:::\s*$/;
const HORIZ_RULE_PATTERN = /^[-*_]{3,}\s*$/;
const TABLE_ROW_PATTERN = /^\|.*\|$/;

const BR_PATTERN = /\n/g;

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeAttribute(value: string): string {
  return escapeHtml(value).replace(/`/g, "&#96;");
}

function sanitizeUrl(url: string): { href: string; attrs: string } {
  const trimmed = url.trim();
  if (!trimmed) return { href: "#", attrs: "" };

  if (/^https?:\/\//i.test(trimmed)) {
    return { href: escapeAttribute(trimmed), attrs: ' target="_blank" rel="noopener noreferrer"' };
  }
  if (/^mailto:/i.test(trimmed) || trimmed.startsWith("/")) {
    return { href: escapeAttribute(trimmed), attrs: "" };
  }
  return { href: "#", attrs: "" };
}

function parseInline(text: string, skipLinks = false): string {
  let output = escapeHtml(text);

  // Backslash escape sequences must be tokenized first so e.g. \` doesn't
  // trigger inline code spans and \* doesn't trigger bold/italic.
  const escTokens: string[] = [];
  output = output.replace(/\\([`*_[\](){}#+\-.!|\\])/g, (_, ch) => {
    const token = `@@ESC${escTokens.length}@@`;
    escTokens.push(ch);
    return token;
  });

  const codeTokens: string[] = [];
  output = output.replace(/`([^`]+)`/g, (_, code) => {
    const token = `@@CODE${codeTokens.length}@@`;
    codeTokens.push(code);
    return token;
  });

  if (!skipLinks) {
    output = output.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, url) => {
      const inner = parseInline(label, true);
      const { href, attrs } = sanitizeUrl(url);
      return `<a href="${href}"${attrs}>${inner}</a>`;
    });
  }

  output = output
    .replace(/(?<!\*)\*\*([^\s*][^*]*?)\*\*(?!\*)/g, "<strong>$1</strong>")
    .replace(/(?<!_)__([^\s_][^_]*?)__(?!_)/g, "<strong>$1</strong>")
    .replace(/(?<!\*)\*([^\s*][^*]*?)\*(?!\*)/g, "<em>$1</em>")
    .replace(/(?<!_)_([^\s_][^_]*?)_(?!_)/g, "<em>$1</em>");

  codeTokens.forEach((code, index) => {
    output = output.replace(`@@CODE${index}@@`, `<code>${code}</code>`);
  });

  // Restore escaped characters last so they are never re-processed
  escTokens.forEach((ch, i) => {
    output = output.replace(`@@ESC${i}@@`, ch);
  });

  return output;
}

function flushParagraph(buffer: string[], html: string[]): void {
  if (!buffer.length) return;
  const raw = buffer.join("\n").trim();
  if (!raw) { buffer.length = 0; return; }
  html.push(`<p>${parseInline(raw).replace(BR_PATTERN, "<br />")}</p>`);
  buffer.length = 0;
}

function flushList(items: string[], html: string[]): void {
  if (!items.length) return;
  html.push(`<ul>${items.map(i => `<li>${i}</li>`).join("")}</ul>`);
  items.length = 0;
}

function flushQuote(lines: string[], html: string[]): void {
  if (!lines.length) return;
  html.push(`<blockquote>${parseInline(lines.join("\n")).replace(BR_PATTERN, "<br />")}</blockquote>`);
  lines.length = 0;
}

function flushTable(rows: string[], html: string[]): void {
  if (rows.length < 2) { rows.length = 0; return; }
  const parsed = rows.map(row =>
    row.replace(/^\||\|$/g, '').split('|').map(c => c.trim())
  );
  const header = parsed[0]!;
  // second row is separator (---, :---:, etc.) — skip it
  const body = parsed.slice(2);
  let table = '<table><thead><tr>';
  table += header.map(h => `<th>${parseInline(h)}</th>`).join('');
  table += '</tr></thead>';
  if (body.length) {
    table += '<tbody>';
    for (const row of body) {
      table += '<tr>' + row.map(c => `<td>${parseInline(c)}</td>`).join('') + '</tr>';
    }
    table += '</tbody>';
  }
  table += '</table>';
  html.push(`<div class="md-table-wrap">${table}</div>`);
  rows.length = 0;
}

function flushAdmonition(type: string, lines: string[], html: string[]): void {
  if (!type) return;
  const normalized = type.toLowerCase().replace(/[\s]+/g, "-");
  const safeType = escapeAttribute(normalized);
  const label = type.charAt(0).toUpperCase() + type.slice(1).toLowerCase();
  const innerHtml = renderMarkdown(lines.join("\n"));
  html.push(`<div class="admonition admonition-${safeType}"><p class="admonition-title">${escapeHtml(label)}</p><div class="admonition-body">${innerHtml}</div></div>`);
  lines.length = 0;
}

export function renderMarkdown(source: string): string {
  if (!source) return "";

  const lines = source.replace(/\r\n/g, "\n").split("\n");
  const html: string[] = [];
  const paragraphBuffer: string[] = [];
  const listItems: string[] = [];
  const quoteLines: string[] = [];
  let inCodeBlock = false;
  let codeLang = "";
  const codeLines: string[] = [];
  let inAdmonition = false;
  let admonitionType = "";
  const admonitionLines: string[] = [];
  const tableRows: string[] = [];

  const flushCodeBlock = () => {
    if (!inCodeBlock) return;
    const langBadge = codeLang ? `<span class="md-lang">${escapeHtml(codeLang)}</span>` : "";
    const copyIcon = CLIPBOARD_SVG;
    const toolbar = `<div class="md-pre-toolbar">${langBadge}<button class="md-copy-btn" type="button">${copyIcon}Copy</button></div>`;
    html.push(`<pre>${toolbar}<code>${escapeHtml(codeLines.join("\n"))}</code></pre>`);
    codeLines.length = 0;
    codeLang = "";
    inCodeBlock = false;
  };

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();

    if (inCodeBlock) {
      if (CODE_FENCE_PATTERN.test(trimmed)) flushCodeBlock();
      else codeLines.push(rawLine);
      continue;
    }
    if (inAdmonition) {
      if (ADMONITION_CLOSE_PATTERN.test(trimmed)) {
        flushAdmonition(admonitionType, admonitionLines, html);
        inAdmonition = false;
        admonitionType = "";
      } else {
        admonitionLines.push(rawLine);
      }
      continue;
    }

    // Table rows
    if (TABLE_ROW_PATTERN.test(trimmed)) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      tableRows.push(trimmed);
      continue;
    } else if (tableRows.length) {
      flushTable(tableRows, html);
    }

    if (!trimmed) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      continue;
    }

    // Horizontal rule (---, ***, ___) — must come before list detection
    if (HORIZ_RULE_PATTERN.test(trimmed) && !LIST_PATTERN.test(trimmed)) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      html.push('<hr />');
      continue;
    }

    if (CODE_FENCE_PATTERN.test(trimmed)) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      inCodeBlock = true;
      codeLang = trimmed.slice(3).trim();
      continue;
    }

    const admonitionMatch = trimmed.match(ADMONITION_OPEN_PATTERN);
    if (admonitionMatch) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      inAdmonition = true;
      admonitionType = admonitionMatch[1]!.toLowerCase();
      continue;
    }

    const headingMatch = trimmed.match(HEADING_PATTERN);
    if (headingMatch) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      flushQuote(quoteLines, html);
      const [, hashes, content] = headingMatch;
      const level = Math.min(hashes!.length, 6);
      html.push(`<h${level}>${parseInline(content!.trim())}</h${level}>`);
      continue;
    }

    if (LIST_PATTERN.test(trimmed)) {
      flushParagraph(paragraphBuffer, html);
      flushQuote(quoteLines, html);
      listItems.push(parseInline(trimmed.replace(LIST_PATTERN, "")));
      continue;
    }

    if (QUOTE_PATTERN.test(trimmed)) {
      flushParagraph(paragraphBuffer, html);
      flushList(listItems, html);
      quoteLines.push(trimmed.replace(QUOTE_PATTERN, ""));
      continue;
    }

    paragraphBuffer.push(rawLine);
  }

  if (inCodeBlock) flushCodeBlock();
  if (inAdmonition) flushAdmonition(admonitionType, admonitionLines, html);
  if (tableRows.length) flushTable(tableRows, html);
  flushParagraph(paragraphBuffer, html);
  flushList(listItems, html);
  flushQuote(quoteLines, html);

  return html.join("\n");
}
