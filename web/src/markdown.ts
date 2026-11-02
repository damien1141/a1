// Markdown rendering for chat bubbles.
//
// marked (GFM + line breaks) with four upgrades over the stock renderer:
//
//   1. Fenced code blocks get a .md-code wrapper: a header row with the
//      language label and a copy button. The content is injected through
//      dangerouslySetInnerHTML, so the button carries no handler of its
//      own — ChatArea attaches one delegated click listener on the
//      transcript container that resolves .md-copy → .md-code → pre>code.
//
//   2. $…$ and $$…$$ math render through KaTeX. A failed parse degrades
//      to the raw source text instead of throwing mid-stream (models
//      legitimately write things like "costs $5 and $10" that only look
//      like math delimiters).
//
//   3. Links to other sites open in a new tab (target=_blank + noopener),
//      so clicking a citation never navigates the chat away.
//
//   4. Task-list checkboxes and tables keep their GFM rendering; the CSS
//      (styles.css .md-content rules) carries the visual polish.

import { marked, type Tokens } from 'marked';
import katex from 'katex';
import 'katex/dist/katex.min.css';

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// renderMath typesets one formula. throwOnError:false makes KaTeX render
// errors inline in red instead of raising; the catch is for the rare case
// where even the error path throws (e.g. pathological input), where the
// raw source beats a missing bubble.
function renderMath(src: string, display: boolean): string {
  try {
    return katex.renderToString(src, {
      displayMode: display,
      throwOnError: false,
      strict: 'ignore',
      output: 'html',
    });
  } catch {
    return escapeHtml(display ? `$$${src}$$` : `$${src}$`);
  }
}

// Math tokenizer notes:
//   - Display math ($$…$$) is tried first and may span newlines (a
//     paragraph block tokenizes its whole text in one inline pass).
//   - Inline math requires a non-space start and end: "$5 and $10" does
//     not match (the content would end with a space), "$x + y$" does.
//   - The (?!\d) after the closing delimiter stops "$20" from pairing
//     with a later "$30" when the content happens to fit.
interface MathToken {
  type: 'math';
  raw: string;
  text: string;
  display: boolean;
}

const mathExtension = {
  name: 'math',
  level: 'inline' as const,
  start(src: string) { return src.indexOf('$'); },
  tokenizer(src: string): MathToken | undefined {
    const display = /^\$\$([\s\S]+?)\$\$/.exec(src);
    if (display && display[0]) {
      return { type: 'math', raw: display[0], text: (display[1] || '').trim(), display: true };
    }
    // No lookbehind (older Safari parses the whole module away on it):
    // the "content must not end with whitespace" rule is a post-check.
    const inline = /^\$(?!\s)((?:\\.|[^\\$])+?)\$(?!\d)/.exec(src);
    if (inline && inline[1] && !/\s$/.test(inline[1])) {
      return { type: 'math', raw: inline[0], text: inline[1], display: false };
    }
    return undefined;
  },
  renderer(token: MathToken) {
    return renderMath(token.text, token.display);
  },
};

// Copy icon (lucide "copy"): inlined as an SVG string because the code
// header lives in the innerHTML payload, where JSX components can't go.
const COPY_ICON =
  '<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" ' +
  'fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" ' +
  'stroke-linejoin="round" aria-hidden="true">' +
  '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/>' +
  '<path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>';

marked.use({
  extensions: [mathExtension as any],
  renderer: {
    // Code blocks: header (language + copy) + scrollable body. The button
    // is purely a target for ChatArea's delegated handler; a click with no
    // JS wired (e.g. a server-rendered fragment) is a harmless no-op.
    code(token: Tokens.Code) {
      const lang = (token.lang || '').trim().split(/\s+/)[0] || 'text';
      const body = token.escaped ? token.text : escapeHtml(token.text);
      return (
        '<div class="md-code">' +
        '<div class="md-code-head">' +
        `<span class="md-code-lang">${escapeHtml(lang)}</span>` +
        '<button type="button" class="md-copy" tabindex="-1" aria-label="Copy code">' +
        `${COPY_ICON}<span class="md-copy-label">copy</span>` +
        '</button>' +
        '</div>' +
        `<pre><code class="language-${escapeHtml(lang)}">${body}</code></pre>` +
        '</div>'
      );
    },
    // Links: external targets open in a new tab; relative/in-page links
    // keep the default behavior.
    link(this: any, token: Tokens.Link) {
      const href: string = token.href || '';
      const text = this.parser.parseInline(token.tokens);
      const title = token.title ? ` title="${escapeHtml(token.title)}"` : '';
      if (/^https?:\/\//i.test(href)) {
        return `<a href="${escapeHtml(href)}"${title} target="_blank" rel="noopener noreferrer">${text}</a>`;
      }
      return `<a href="${escapeHtml(href)}"${title}>${text}</a>`;
    },
  },
});

marked.setOptions({
  breaks: true,
  gfm: true,
});

export function renderMarkdown(text: string): string {
  return marked.parse(text, { async: false }) as string;
}
