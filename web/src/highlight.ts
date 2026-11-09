// highlight.js, vendored the sparse way (the anti-goal's ceiling lets this
// in; nothing else follows). The server's filesapi owns the extension→
// language ruling and sends the id in every read response — the client
// registers exactly that curated set and nothing more, so an unmapped
// extension previews as plaintext on both sides of the wire by the same
// rule. Core (no bundled languages) + ~30 explicit imports keeps the
// bundle at the languages the browser can actually meet.

import hljs from 'highlight.js/lib/core';
import go from 'highlight.js/lib/languages/go';
import typescript from 'highlight.js/lib/languages/typescript';
import javascript from 'highlight.js/lib/languages/javascript';
import python from 'highlight.js/lib/languages/python';
import rust from 'highlight.js/lib/languages/rust';
import ruby from 'highlight.js/lib/languages/ruby';
import java from 'highlight.js/lib/languages/java';
import c from 'highlight.js/lib/languages/c';
import cpp from 'highlight.js/lib/languages/cpp';
import csharp from 'highlight.js/lib/languages/csharp';
import kotlin from 'highlight.js/lib/languages/kotlin';
import swift from 'highlight.js/lib/languages/swift';
import php from 'highlight.js/lib/languages/php';
import lua from 'highlight.js/lib/languages/lua';
import json from 'highlight.js/lib/languages/json';
import yaml from 'highlight.js/lib/languages/yaml';
import ini from 'highlight.js/lib/languages/ini';
import markdown from 'highlight.js/lib/languages/markdown';
import css from 'highlight.js/lib/languages/css';
import scss from 'highlight.js/lib/languages/scss';
import xml from 'highlight.js/lib/languages/xml';
import bash from 'highlight.js/lib/languages/bash';
import sql from 'highlight.js/lib/languages/sql';
import diff from 'highlight.js/lib/languages/diff';
import protobuf from 'highlight.js/lib/languages/protobuf';
import makefile from 'highlight.js/lib/languages/makefile';
import dockerfile from 'highlight.js/lib/languages/dockerfile';

// The set mirrors filesapi.go's fileLanguages VALUES (plus the two
// extensionless basenames) — one ruling, two sides, no drift. If the
// server ever maps a new id this side does not register, the preview
// falls back to plaintext rather than guessing (getLanguage guard below).
// Three ids ship no grammar of their own in highlight.js and register as
// aliases instead: tsx/jsx get the TS/JS grammars (they cover all but
// the JSX tags themselves), and toml gets ini — TOML is ini's dialect
// (sections, key = value, # comments all light up) and that is the
// honest best available.
const languages = {
  go, typescript, javascript, python, rust, ruby, java, c, cpp,
  csharp, kotlin, swift, php, lua, json, yaml, ini, markdown, css,
  scss, xml, bash, sql, diff, protobuf, makefile, dockerfile,
} as const;

for (const [id, grammar] of Object.entries(languages)) {
  hljs.registerLanguage(id, grammar as Parameters<typeof hljs.registerLanguage>[1]);
}
hljs.registerLanguage('tsx', typescript);
hljs.registerLanguage('jsx', javascript);
hljs.registerLanguage('toml', ini);

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// highlightCode returns HTML (already escaped) safe for
// dangerouslySetInnerHTML. An unknown or empty language, or a grammar that
// throws on pathological input, degrades to escaped plaintext — a preview
// that is plain beats a preview that is missing.
export function highlightCode(code: string, lang: string): string {
  if (!lang || !hljs.getLanguage(lang)) return escapeHtml(code);
  try {
    return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(code);
  }
}
