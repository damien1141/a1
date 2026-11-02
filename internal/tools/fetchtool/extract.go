package fetchtool

// This file is the extraction + rendering half of the fetch tool: HTML →
// text, page metadata, heading outline, link extraction with anchor text,
// JSON pretty-print, and the block / multi-URL renderers. The HTTP layer +
// tool definition live in fetchtool.go; this file owns the post-fetch
// parsing.
//
// All extraction is regex-based, not a real HTML parser. That's deliberate:
// the fetch tool must stay dependency-free, the input is untrusted HTML from
// arbitrary URLs, and the regexes are loose enough to handle the messy
// real-world markup that a strict parser would reject. The cost is that
// malformed HTML (unclosed tags, attribute values with > inside, etc.) can
// confuse the matchers — but for the agent's use case (skim the page, find
// links, get the title) the loose match is more useful than a strict
// reject.

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/url"
        "regexp"
        "sort"
        "strings"
)

// defaultMaxChars is the body-text cap when the caller passes maxChars=0.
// 8K is enough for the agent to skim a page and decide whether to follow
// up; the caller can raise it via the max_chars param or set it negative
// for unlimited.
const defaultMaxChars = 8 << 10 // 8 KiB

// prePlaceholderPrefix is the sentinel that htmlToText swaps in for each
// extracted <pre> block. It must survive the block-tags → newline step, the
// strip-all-tags step, and the per-line collapse-whitespace step. A NUL-
// framed token with no surrounding whitespace is the safest choice: NUL
// can't appear in HTML text (it would have been encoded), and the token
// has no leading/trailing whitespace for collapse-whitespace to trim.
const prePlaceholderPrefix = "\x00PRE_"

// === HTML to text ===

// htmlToText strips HTML tags and returns readable text. It preserves line
// breaks around block-level elements and collapses whitespace to keep the
// output token-friendly.
//
// maxChars controls truncation: 0 = default (8K), negative = unlimited,
// positive = truncate at N chars with a [truncated] marker.
//
// <pre> blocks are special: their internal whitespace (newlines, leading
// spaces) is meaningful and would be destroyed by the collapse-whitespace
// step. They are extracted up front, replaced with a sentinel, and restored
// after the rest of the pipeline runs — wrapped in a ``` markdown fence so
// the agent sees them as code blocks.
func htmlToText(html string, maxChars int) string {
        // Extract <pre> blocks first so their whitespace survives the rest of
        // the pipeline. Nested tags inside <pre> (e.g. <code>, <span>) are
        // stripped, but newlines and indentation are preserved verbatim.
        var preBlocks []string
        html = preRe.ReplaceAllStringFunc(html, func(match string) string {
                sub := preRe.FindStringSubmatch(match)
                if len(sub) < 2 {
                        return match
                }
                content := tagRe.ReplaceAllString(sub[1], "")
                content = decodeEntities(content)
                // Trim trailing newlines so the fence doesn't have a blank line
                // at the bottom, but keep internal newlines + indentation.
                content = strings.TrimRight(content, "\n")
                preBlocks = append(preBlocks, "```\n"+content+"\n```")
                return fmt.Sprintf("%s%d\x00", prePlaceholderPrefix, len(preBlocks)-1)
        })

        // Replace block-level tags with newlines to preserve structure.
        text := blockTagsRe.ReplaceAllString(html, "\n")

        // Remove all remaining HTML tags.
        text = tagRe.ReplaceAllString(text, "")

        // Decode common HTML entities.
        text = decodeEntities(text)

        // Collapse whitespace: trim each line and remove blank lines.
        lines := strings.Split(text, "\n")
        var cleaned []string
        for _, line := range lines {
                line = strings.TrimSpace(line)
                if line != "" {
                        cleaned = append(cleaned, line)
                }
        }
        text = strings.Join(cleaned, "\n")

        // Restore <pre> blocks. The placeholder is on its own line (the
        // surrounding block tags became \n), so the replacement expands it into
        // a multi-line fenced block without affecting surrounding lines.
        for i, block := range preBlocks {
                text = strings.ReplaceAll(text, fmt.Sprintf("%s%d\x00", prePlaceholderPrefix, i), block)
        }

        // Apply truncation. maxChars<=0 means default; maxChars<0 means no
        // truncation; positive means truncate at N.
        limit := maxChars
        if limit == 0 {
                limit = defaultMaxChars
        }
        if limit > 0 && len(text) > limit {
                text = text[:limit] + "\n\n[truncated]"
        }
        return text
}

// === JSON pretty-print ===

// prettyPrintJSON parses s as JSON and re-marshals it with 2-space indent
// so the agent can read it without mentally re-indenting. If s isn't valid
// JSON (e.g. server lied about Content-Type), the raw string is returned
// unchanged — better to give the agent the bytes than to fail the fetch.
//
// maxChars controls truncation the same way htmlToText does.
func prettyPrintJSON(s string, maxChars int) string {
        var v any
        if err := json.Unmarshal([]byte(s), &v); err != nil {
                return s
        }
        pretty, err := json.MarshalIndent(v, "", "  ")
        if err != nil {
                return s
        }
        out := string(pretty)
        limit := maxChars
        if limit == 0 {
                limit = defaultMaxChars
        }
        if limit > 0 && len(out) > limit {
                out = out[:limit] + "\n\n[truncated]"
        }
        return out
}

// === Metadata extraction ===

var (
        // titleRe captures <title>…</title>. (?is) makes it case-insensitive and
        // lets . match newlines so multi-line titles work.
        titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
        // metaBlockRe captures one <meta ...> tag's attribute list; parseAttrs
        // then extracts name/property/content from that list.
        metaBlockRe = regexp.MustCompile(`(?is)<meta\s+([^>]+)>`)
        // attrRe captures key="value" pairs inside a tag's attribute list.
        attrRe = regexp.MustCompile(`(?is)(\w+)\s*=\s*"([^"]*)"`)
        // canonicalRe captures <link rel="canonical" href="…">.
        canonicalRe = regexp.MustCompile(`(?is)<link\s+[^>]*?rel\s*=\s*"canonical"[^>]*?href\s*=\s*"([^"]+)"`)
        // timeRe captures <time datetime="…"> as a fallback for published dates.
        timeRe = regexp.MustCompile(`(?is)<time\s+[^>]*?datetime\s*=\s*"([^"]+)"`)
)

// pageMetadata is the extracted metadata block. Empty fields are omitted
// by renderMetadata, so the agent only sees what the page actually
// declared.
type pageMetadata struct {
        Title         string
        Description   string
        Canonical     string
        SiteName      string
        PageType      string
        OGTitle       string
        OGDescription string
        Author        string
        PublishedTime string
}

// extractMetadata parses the raw HTML for page metadata. baseURL is used
// to resolve relative canonical URLs.
func extractMetadata(html, baseURL string) pageMetadata {
        var m pageMetadata
        if s := titleRe.FindStringSubmatch(html); len(s) > 1 {
                m.Title = strings.TrimSpace(decodeEntities(stripTags(s[1])))
        }
        for _, b := range metaBlockRe.FindAllStringSubmatch(html, -1) {
                attrs := parseAttrs(b[1])
                name := attrs["name"]
                prop := attrs["property"]
                content := attrs["content"]
                if content == "" {
                        continue
                }
                switch {
                case name == "description":
                        m.Description = content
                case name == "author":
                        m.Author = content
                case prop == "og:title":
                        m.OGTitle = content
                case prop == "og:description":
                        m.OGDescription = content
                case prop == "og:site_name":
                        m.SiteName = content
                case prop == "og:type":
                        m.PageType = content
                case prop == "article:published_time":
                        m.PublishedTime = content
                }
        }
        if c := canonicalRe.FindStringSubmatch(html); len(c) > 1 {
                m.Canonical = resolveURL(baseURL, c[1])
        }
        if m.PublishedTime == "" {
                if t := timeRe.FindStringSubmatch(html); len(t) > 1 {
                        m.PublishedTime = t[1]
                }
        }
        return m
}

// parseAttrs parses a string of key="value" pairs into a map. Keys are
// lowercased so the caller can compare against "name", "property", etc.
// without worrying about case.
func parseAttrs(s string) map[string]string {
        out := map[string]string{}
        for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
                out[strings.ToLower(m[1])] = m[2]
        }
        return out
}

// resolveURL resolves a possibly-relative URL against base. Returns the
// raw ref if resolution fails so the renderer never shows an empty string.
func resolveURL(base, ref string) string {
        b, err := url.Parse(base)
        if err != nil {
                return ref
        }
        u, err := b.Parse(ref)
        if err != nil || u.Scheme == "" {
                return ref
        }
        return u.String()
}

// renderMetadata formats the metadata block. Empty fields are omitted so
// the agent doesn't see a wall of "Title: \nDescription: \n…" lines.
func renderMetadata(m pageMetadata) string {
        var sb strings.Builder
        writeMeta := func(label, val string) {
                if val == "" {
                        return
                }
                sb.WriteString(fmt.Sprintf("%s: %s\n", label, val))
        }
        writeMeta("Title", m.Title)
        writeMeta("Description", m.Description)
        writeMeta("Canonical", m.Canonical)
        writeMeta("SiteName", m.SiteName)
        writeMeta("Type", m.PageType)
        writeMeta("Author", m.Author)
        writeMeta("Published", m.PublishedTime)
        return sb.String()
}

// === Outline extraction ===

// headingRe captures <h1>…</h1> through <h6>…</h6> in document order. RE2
// (Go's regexp) doesn't support backreferences, so the close tag is its
// own h[1-6] class — a malformed <h2>…</h3> would match the inner span up
// to the next </h?>, which is acceptable for skim-style outline extraction.
var headingRe = regexp.MustCompile(`(?is)<(h[1-6])[^>]*>(.*?)</h[1-6]>`)

// headingEntry is one h1-h6 heading in the outline.
type headingEntry struct {
        Level int
        Text  string
}

// extractOutline finds every h1-h6 heading in document order. Nested tags
// inside the heading (e.g. <a>, <span>) are stripped.
func extractOutline(html string) []headingEntry {
        var out []headingEntry
        for _, m := range headingRe.FindAllStringSubmatch(html, -1) {
                if len(m[1]) == 0 {
                        continue
                }
                level := int(m[1][1] - '0') // '1' → 1, '6' → 6
                if level < 1 || level > 6 {
                        continue
                }
                text := strings.TrimSpace(decodeEntities(stripTags(m[2])))
                if text == "" {
                        continue
                }
                if len(text) > 200 {
                        text = text[:200] + "..."
                }
                out = append(out, headingEntry{Level: level, Text: text})
        }
        return out
}

// renderOutline formats the heading outline as an indented tree. h1 sits
// at column 0, h2 at column 2, h3 at column 4, etc. — so the agent can
// scan the page's structure at a glance.
func renderOutline(headings []headingEntry) string {
        var sb strings.Builder
        for _, h := range headings {
                indent := strings.Repeat("  ", h.Level-1)
                sb.WriteString(fmt.Sprintf("%s- %s\n", indent, h.Text))
        }
        return sb.String()
}

// === Link extraction (extended with anchor text) ===

var (
        // linkRe matches any href / src attribute. Used as a fallback for
        // non-anchor links (link, script, img, etc.) where there is no anchor
        // text to capture.
        linkRe = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*"([^"]+)"`)
        // anchorRe matches <a href="…">text</a> blocks, capturing both the URL
        // and the (possibly tag-containing) anchor text. (?is) makes it
        // case-insensitive and dot-matches-newline so multi-line anchors work.
        anchorRe = regexp.MustCompile(`(?is)<a\b[^>]*?\bhref\s*=\s*"([^"]+)"[^>]*>(.*?)</a>`)
        // tagRe matches any HTML tag, used to strip tags from captured text.
        tagRe = regexp.MustCompile(`(?i)<[^>]+>`)
        // blockTagsRe matches closing block-level tags so the converter can
        // replace them with newlines, preserving structural line breaks.
        blockTagsRe = regexp.MustCompile(
                `(?i)</(p|div|section|article|header|footer|main|nav|aside|ul|ol|li|table|tr|td|th|h[1-6]|blockquote|pre|code|br|hr)[^>]*>\s*`,
        )
        // preRe matches <pre>…</pre> blocks for whitespace-preserving extraction.
        // Non-greedy so adjacent <pre> blocks match separately.
        preRe = regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)
)

// linkEntry is one resolved link, grouped by host for the agent's
// navigation. Text is the anchor text for <a> links, empty for non-anchor
// hrefs/srcs.
type linkEntry struct {
        Host string
        Path string
        Text string
}

// extractLinks finds every href/src in the raw HTML, resolves them against
// the page URL, and returns the unique set grouped by host. Anchor text is
// captured when the link is an <a> element; non-anchor hrefs/srcs get an
// empty Text.
//
// Pass 1 (anchorRe) finds <a href="…">text</a> pairs first so the anchor
// text is preserved. Pass 2 (linkRe) sweeps the remaining hrefs/srcs that
// weren't already captured by pass 1 — <link rel="canonical" href>,
// <script src>, <img src>, etc. — and adds them with empty Text. The
// `seen` map (keyed by host+path) is shared between passes so the second
// pass skips anything pass 1 already recorded.
func extractLinks(html, baseURL string) []linkEntry {
        base, err := url.Parse(baseURL)
        if err != nil {
                return nil
        }
        seen := make(map[string]struct{})
        var out []linkEntry

        // Pass 1: <a href="…">text</a>.
        for _, m := range anchorRe.FindAllStringSubmatch(html, -1) {
                raw := strings.TrimSpace(m[1])
                if raw == "" || !shouldFollow(raw) {
                        continue
                }
                u, err := base.Parse(raw)
                if err != nil || u.Scheme == "" || (u.Scheme != "http" && u.Scheme != "https") {
                        continue
                }
                key := u.Host + u.Path
                if _, ok := seen[key]; ok {
                        continue
                }
                seen[key] = struct{}{}
                text := strings.TrimSpace(decodeEntities(stripTags(m[2])))
                if len(text) > 100 {
                        text = text[:100] + "..."
                }
                out = append(out, linkEntry{Host: u.Host, Path: u.Path, Text: text})
        }

        // Pass 2: remaining hrefs/srcs (non-anchor) with empty text.
        for _, m := range linkRe.FindAllStringSubmatch(html, -1) {
                raw := strings.TrimSpace(m[1])
                if raw == "" || !shouldFollow(raw) {
                        continue
                }
                u, err := base.Parse(raw)
                if err != nil || u.Scheme == "" || (u.Scheme != "http" && u.Scheme != "https") {
                        continue
                }
                key := u.Host + u.Path
                if _, ok := seen[key]; ok {
                        continue
                }
                seen[key] = struct{}{}
                out = append(out, linkEntry{Host: u.Host, Path: u.Path})
        }

        // Sort by host so the output is scannable: subdomains group together.
        sort.Slice(out, func(i, j int) bool {
                if out[i].Host != out[j].Host {
                        return out[i].Host < out[j].Host
                }
                return out[i].Path < out[j].Path
        })
        return out
}

// shouldFollow skips non-navigable schemes: mailto, tel, javascript:, etc.
// Anything with a scheme other than http/https (or a schemeless //host
// inherit-from-base form) is rejected.
func shouldFollow(raw string) bool {
        if strings.Contains(raw, ":") && !strings.HasPrefix(raw, "http") && !strings.HasPrefix(raw, "//") {
                return false
        }
        return true
}

// formatLinks renders the link map as a compact, scannable block: one host
// header followed by its paths. Subdomains sort naturally under the parent
// host, so the agent sees the site's structure at a glance. Anchor text
// (when present) appears after the path, separated by an em dash.
func formatLinks(links []linkEntry) string {
        var sb strings.Builder
        var cur string
        count := 0
        for _, l := range links {
                if l.Host != cur {
                        cur = l.Host
                        sb.WriteString(fmt.Sprintf("\n%s\n", cur))
                        count++
                }
                p := l.Path
                if p == "" {
                        p = "/"
                }
                if l.Text != "" {
                        sb.WriteString(fmt.Sprintf("  %s — %s\n", p, l.Text))
                } else {
                        sb.WriteString(fmt.Sprintf("  %s\n", p))
                }
        }
        sb.WriteString(fmt.Sprintf("\n(%d links across %d hosts)", len(links), count))
        return sb.String()
}

// stripTags removes all HTML tags from s. Used to clean anchor text and
// heading text captured by the loose regexes.
func stripTags(s string) string {
        return tagRe.ReplaceAllString(s, "")
}

// decodeEntities decodes the common HTML entities that appear in titles,
// descriptions, and anchor text. Numeric entities are not decoded — the
// regex-based extraction handles the named set that covers ~99% of real
// pages, and a full entity decoder would pull in a heavier dependency.
func decodeEntities(s string) string {
        s = strings.ReplaceAll(s, "&nbsp;", " ")
        s = strings.ReplaceAll(s, "&lt;", "<")
        s = strings.ReplaceAll(s, "&gt;", ">")
        s = strings.ReplaceAll(s, "&amp;", "&")
        s = strings.ReplaceAll(s, "&quot;", "\"")
        s = strings.ReplaceAll(s, "&#39;", "'")
        return s
}

// collapseWhitespace replaces runs of whitespace (including newlines and
// tabs) with a single space so extracted text reads as prose. Used by the
// search-mode snippet parser to flatten DDG's multi-line result-snippet
// table cells into a single-line snippet.
//
// Mirrors the same-named helper in browsertool; defined locally so this
// package doesn't import browsertool (which would create a cycle: the
// harness registers both fetch and browser tools, and pulling browsertool
// into fetchtool would couple them at the package level).
func collapseWhitespace(s string) string {
        var sb strings.Builder
        sb.Grow(len(s))
        inSpace := false
        for _, r := range s {
                if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
                        if !inSpace {
                                sb.WriteRune(' ')
                                inSpace = true
                        }
                        continue
                }
                inSpace = false
                sb.WriteRune(r)
        }
        return strings.TrimSpace(sb.String())
}

// === Rendering ===

// renderBlock formats a single fetch result. The status line stays in
// Content — the model needs it to reason about the response — and the
// metadata / outline / links sections appear before the body so the agent
// can scan them before reading the (potentially long) body text.
//
// Final URL is shown when it differs from the requested URL (i.e. the
// client followed a redirect); Location is shown whenever present (most
// useful in redirects=manual mode, but also surfaces canonical URLs some
// servers set on 2xx).
func renderBlock(res fetchResult) string {
        var sb strings.Builder
        if res.err != nil {
                sb.WriteString(fmt.Sprintf("URL: %s\nError: %v\n", res.url, res.err))
                return sb.String()
        }
        sb.WriteString(fmt.Sprintf("Status: %d %s\n", res.statusCode, http.StatusText(res.statusCode)))
        if ct := res.contentType; ct != "" {
                sb.WriteString(fmt.Sprintf("Content-Type: %s\n", ct))
        }
        sb.WriteString(fmt.Sprintf("URL: %s\n", res.url))
        if res.finalURL != "" && res.finalURL != res.url {
                sb.WriteString(fmt.Sprintf("Final URL: %s\n", res.finalURL))
        }
        if res.location != "" {
                sb.WriteString(fmt.Sprintf("Location: %s\n", res.location))
        }
        if res.metadata != nil {
                sb.WriteString("\n## Metadata\n\n")
                sb.WriteString(renderMetadata(*res.metadata))
        }
        if len(res.outline) > 0 {
                sb.WriteString("\n## Outline\n\n")
                sb.WriteString(renderOutline(res.outline))
        }
        sb.WriteString("\n")
        sb.WriteString(res.bodyText)
        if len(res.links) > 0 {
                sb.WriteString("\n\n## Links\n\n")
                sb.WriteString(formatLinks(res.links))
        }
        sb.WriteString("\n")
        return sb.String()
}

// renderMultiResult formats the multi-URL fetch result. Per-URL blocks are
// separated by a '---' rule so the agent can tell where one page ends and
// the next begins; the header line names the count.
func renderMultiResult(urls []string, results []fetchResult) string {
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("Fetched %d URLs\n", len(urls)))
        sb.WriteString(strings.Repeat("=", 60))
        sb.WriteString("\n\n")
        for i, res := range results {
                if i > 0 {
                        sb.WriteString("\n")
                        sb.WriteString(strings.Repeat("-", 60))
                        sb.WriteString("\n\n")
                }
                sb.WriteString(renderBlock(res))
        }
        return sb.String()
}
