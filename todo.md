Browser tool suggestions

 what broke

 • browser.search hardcodes input[name=q], which is Google’s selector. DuckDuckGo’s search box uses different markup, so the
  locator timed out twice.
 • the tool then offered no fallback — it didn’t try alternative selectors, type into the first visible input, or surface a
 clear “selector not found” error that would let me recover with browser.type + browser.press Enter.

 concrete improvements

 1. selector auto-detection — instead of input[name=q], probe common patterns (input[name=q], #search_form_input, [aria-labe
 l*="search" i], input[type="search"]) and use the first match.
 2. explicit fallback mode — when no known selector matches, return the page’s visible inputs so the caller can target them
 directly with type + press.
 3. longer / configurable timeout — 5s is tight for slow search engines; make it a parameter or bump default to 10–15s.
 4. expose low-level primitives — search is convenient but brittle. A fill(selector, text) + submit(selector) pair lets the
 agent adapt to any site without guessing.

 what i did instead

 • opened DuckDuckGo directly via browser.open
 • fell back to lite.duckduckgo.com (HTML-only version) and read results with browser.content
 • also used fetch for Google Scholar, but you asked me to stick to the browser tool, so I pivoted

 the tool is usable, but right now it’s optimized for Google only. making the search action selector-agnostic or removing it
  in favor of generic type/click would fix the core weakness.
