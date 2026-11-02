# Patterns & Components

Reference implementations for the recurring UI patterns. Each is accessible (keyboard nav, ARIA, focus management), uses `x-cloak` to prevent FOUC, and animates with cubic-bezier (never `linear` for state).

## Dropdown / menu

```html
<div x-data="{ open: false }" class="relative">
  <button type="button" @click="open = !open"
          :aria-expanded="open.toString()" aria-haspopup="menu">
    Account
  </button>
  <div x-show="open" x-cloak role="menu"
       x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200"
       x-transition:enter-start="opacity-0 -translate-y-2"
       x-transition:enter-end="opacity-100 translate-y-0"
       x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-150"
       x-transition:leave-start="opacity-100 translate-y-0"
       x-transition:leave-end="opacity-0 -translate-y-2"
       @click.outside="open = false"
       @keydown.escape.window="open = false"
       class="absolute right-0 mt-2 w-48 rounded-md border bg-white shadow-lg">
    <a href="/profile" role="menuitem" class="block px-4 py-2 hover:bg-gray-50">Profile</a>
    <a href="/settings" role="menuitem" class="block px-4 py-2 hover:bg-gray-50">Settings</a>
    <button type="button" role="menuitem" @click="logout()"
            class="block w-full text-left px-4 py-2 hover:bg-gray-50">Sign out</button>
  </div>
</div>
```
Must-haves: `:aria-expanded`, `@click.outside`, `@keydown.escape.window`, explicit `x-transition`.

## Modal (focus trap + Escape + scroll lock)

```html
<div x-data="modal()" x-modelable="open" x-model="parentOpen">
  <button @click="open = true" aria-haspopup="dialog">Open</button>

  <dialog x-show="open" x-cloak
          x-trap="open"
          x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
          x-transition:enter-start="opacity-0"
          x-transition:enter-end="opacity-100"
          @click.outside="close()"
          @keydown.escape.window="close()"
          role="dialog" aria-modal="true" aria-labelledby="modal-title"
          class="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4">
    <div class="bg-white rounded-xl max-w-md w-full p-6 shadow-xl">
      <h2 id="modal-title" class="text-lg font-semibold">Confirm</h2>
      <p class="mt-2 text-gray-600">Are you sure you want to delete this?</p>
      <div class="mt-6 flex justify-end gap-3">
        <button x-ref="cancel" @click="close()" class="px-4 py-2">Cancel</button>
        <button @click="confirm()" class="px-4 py-2 bg-red-600 text-white rounded">Delete</button>
      </div>
    </div>
  </dialog>
</div>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('modal', () => ({
      open: false,
      lastFocus: null,
      scrollLocked: false,
      init() {
        this.$watch('open', (v) => {
          if (v) this.onOpen();
          else this.onClose();
        });
      },
      onOpen() {
        this.lastFocus = document.activeElement;
        document.body.style.overflow = 'hidden';
        this.scrollLocked = true;
        this.$nextTick(() => this.$refs.cancel?.focus());
      },
      onClose() {
        if (this.scrollLocked) {
          document.body.style.overflow = '';
          this.scrollLocked = false;
        }
        this.lastFocus?.focus();
      },
      close() { this.open = false; },
      confirm() { this.$dispatch('confirmed'); this.close(); },
    }));
  });
</script>
```
Requires `@alpinejs/focus` for `x-trap`. Without it, hand-roll focus cycling with Tab/Shift+Tab in a `@keydown.tab.prevent` handler.

## Tabs

```html
<div x-data="tabs(0)" class="border-b">
  <div role="tablist" class="flex gap-1">
    <button x-for="(tab, i) in panels" :key="tab.id"
            @click="select(i)"
            @keydown.arrow-right.prevent="select((active + 1) % panels.length)"
            @keydown.arrow-left.prevent="select((active - 1 + panels.length) % panels.length)"
            :aria-selected="(active === i).toString()"
            :tabindex="active === i ? 0 : -1"
            :id="`${tab.id}-tab`"
            :aria-controls="`${tab.id}-panel`"
            role="tab"
            x-text="tab.label"
            class="px-4 py-2 border-b-2"
            :class="active === i ? 'border-blue-500' : 'border-transparent'"></button>
  </div>
  <div x-for="(tab, i) in panels" :key="tab.id"
       x-show="active === i" x-cloak
       :id="`${tab.id}-panel`"
       :aria-labelledby="`${tab.id}-tab`"
       role="tabpanel"
       class="p-4"
       x-text="tab.content"></div>
</div>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('tabs', (initial = 0) => ({
      active: initial,
      panels: [
        { id: 'overview', label: 'Overview', content: 'Overview content' },
        { id: 'details',  label: 'Details',  content: 'Details content' },
        { id: 'reviews',  label: 'Reviews',  content: 'Reviews content' },
      ],
      select(i) { this.active = i; },
    }));
  });
</script>
```
Must-haves: `role="tablist"`/`tab`/`tabpanel`, `aria-selected`, `aria-controls`/`aria-labelledby`, arrow-key navigation, `tabindex` roving.

## Accordion (CSS grid rows trick — no plugin)

```html
<div x-data="{ open: null }" class="divide-y">
  <template x-for="(item, i) in items" :key="item.id">
    <div>
      <button @click="open = open === i ? null : i"
              :aria-expanded="(open === i).toString()"
              :aria-controls="`panel-${item.id}`"
              class="w-full text-left py-4 flex justify-between">
        <span x-text="item.title"></span>
        <span x-text="open === i ? '−' : '+'" aria-hidden="true"></span>
      </button>
      <div :id="`panel-${item.id}`" role="region"
           x-show="open === i" x-collapse
           class="overflow-hidden">
        <p class="py-4 text-gray-600" x-text="item.body"></p>
      </div>
    </div>
  </template>
</div>
<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('accordionData', () => ({
      open: null,
      items: [/* ... */],
    }));
  });
</script>
```
Uses `@alpinejs/collapse` for smooth height. Single-open accordion: `open` holds an index. Multi-open: replace `open: null` with `open: new Set()` and toggle.

## Toasts (global store + container)

```html
<body x-data>
  <div class="fixed bottom-4 right-4 z-50 space-y-2" aria-live="polite">
    <template x-for="t in $store.toasts.items" :key="t.id">
      <div x-show="t.show" x-cloak
           x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
           x-transition:enter-start="opacity-0 translate-x-8"
           x-transition:enter-end="opacity-100 translate-x-0"
           x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200"
           x-transition:leave-start="opacity-100 translate-x-0"
           x-transition:leave-end="opacity-0 translate-x-8"
           :class="t.type === 'error' ? 'bg-red-600 text-white' : 'bg-gray-900 text-white'"
           class="px-4 py-3 rounded-lg shadow-lg max-w-sm">
        <p x-text="t.message"></p>
      </div>
    </template>
  </div>
</body>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.store('toasts', {
      items: [],
      push(message, type = 'info', ms = 3_000) {
        const id = Date.now() + Math.random();
        const t = { id, message, type, show: false };
        this.items.push(t);
        this.$nextTick(() => { t.show = true; }); // trigger transition
        setTimeout(() => {
          t.show = false;
          setTimeout(() => {
            this.items = this.items.filter((x) => x.id !== id);
          }, 300); // wait for leave transition
        }, ms);
      },
      error(m) { this.push(m, 'error'); },
      success(m) { this.push(m, 'success'); },
    });
  });

  // Usage from anywhere:
  // Alpine.store('toasts').success('Saved!');
  // Or from an htmx response header:
  document.body.addEventListener('toast', (e) => Alpine.store('toasts').push(e.detail.message));
</script>
```

## Typeahead / search

```html
<div x-data="typeahead()" class="relative">
  <input type="search"
         x-model="query"
         @input.debounce.200ms="search()"
         @keydown.arrow-down.prevent="highlight = Math.min(highlight + 1, results.length - 1)"
         @keydown.arrow-up.prevent="highlight = Math.max(highlight - 1, -1)"
         @keydown.enter.prevent="select(highlight)"
         @keydown.escape.window="close()"
         :aria-expanded="(results.length > 0).toString()"
         aria-controls="ta-list"
         aria-autocomplete="list"
         role="combobox"
         placeholder="Search…" />
  <ul x-show="results.length > 0" x-cloak id="ta-list" role="listbox"
      @click.outside="close()"
      class="absolute mt-1 w-full border rounded shadow-lg bg-white max-h-60 overflow-auto">
    <template x-for="(r, i) in results" :key="r.id">
      <li role="option" :aria-selected="(highlight === i).toString()"
          @mouseenter="highlight = i" @click="select(i)"
          :class="highlight === i ? 'bg-blue-100' : ''"
          class="px-3 py-2 cursor-pointer" x-text="r.label"></li>
    </template>
  </ul>
</div>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('typeahead', () => ({
      query: '', results: [], highlight: -1,
      async search() {
        if (!this.query.trim()) { this.results = []; return; }
        this.results = await fetch(`/api/search?q=${encodeURIComponent(this.query)}`).then(r => r.json());
        this.highlight = -1;
      },
      select(i) {
        if (i < 0 || i >= this.results.length) return;
        const r = this.results[i];
        this.$dispatch('selected', r);
        this.query = r.label;
        this.close();
      },
      close() { this.results = []; this.highlight = -1; },
    }));
  });
</script>
```
Must-haves: `@input.debounce.200ms`, arrow-key nav, `aria-autocomplete`, `role="listbox"`/`option`, `@click.outside` close.

## Infinite scroll

```html
<div x-data="feed()">
  <template x-for="item in items" :key="item.id">
    <article class="py-4 border-b" x-text="item.title"></article>
  </template>
  <div x-intersect.once="loadMore()" x-show="loading" class="py-4 text-center">
    Loading…
  </div>
</div>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('feed', () => ({
      items: [], page: 1, loading: false, hasMore: true,
      async loadMore() {
        if (this.loading || !this.hasMore) return;
        this.loading = true;
        const res = await fetch(`/api/feed?page=${this.page}`).then(r => r.json());
        this.items.push(...res.items);
        this.hasMore = res.hasMore;
        this.page++;
        this.loading = false;
      },
    }));
  });
</script>
```
For htmx-driven infinite scroll, use `hx-trigger="revealed"` on the sentinel instead — see `htmx-pro` skill.

## Form validation

```html
<form x-data="form()" @submit.prevent="submit()">
  <label for="email">Email</label>
  <input id="email" type="email" x-model="email"
         @blur="touched.email = true"
         :class="errors.email ? 'border-red-500' : 'border-gray-300'" />
  <p x-show="touched.email && errors.email" x-text="errors.email" x-cloak
     class="text-red-600 text-sm"></p>

  <button type="submit" :disabled="!isValid">Submit</button>
</form>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('form', () => ({
      email: '', touched: { email: false },
      get errors() {
        const e = {};
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(this.email)) e.email = 'Invalid email';
        return e;
      },
      get isValid() { return Object.keys(this.errors).length === 0; },
      async submit() {
        this.touched.email = true;
        if (!this.isValid) return;
        await fetch('/api/subscribe', { method: 'POST', body: JSON.stringify({ email: this.email }) });
        Alpine.store('toasts').success('Subscribed!');
      },
    }));
  });
</script>
```
Pattern: errors as a getter (recomputed reactively), `touched` to gate display, `@blur` to mark touched.

## Drag-and-drop (HTML5 DnD)

```html
<ul x-data="kanban()">
  <template x-for="(card, i) in cards" :key="card.id">
    <li draggable="true"
        @dragstart="dragIndex = i"
        @dragover.prevent
        @drop="reorder(i)"
        class="cursor-move p-2 border-b" x-text="card.title"></li>
  </template>
</ul>

<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('kanban', () => ({
      cards: [{ id: 1, title: 'A' }, { id: 2, title: 'B' }, { id: 3, title: 'C' }],
      dragIndex: null,
      reorder(toIndex) {
        if (this.dragIndex === null) return;
        const [moved] = this.cards.splice(this.dragIndex, 1);
        this.cards.splice(toIndex, 0, moved);
        this.dragIndex = null;
      },
    }));
  });
</script>
```
For richer DnD (handles, cross-list, custom preview), use `@alpinejs/sort` or SortableJS.

## Quick reference

| Pattern | Key directives | ARIA |
|---|---|---|
| Dropdown | `@click.outside` `@keydown.escape.window` `:aria-expanded` | `role="menu"` `role="menuitem"` `aria-haspopup` |
| Modal | `x-trap` `@click.outside` `@keydown.escape.window` scroll lock | `role="dialog"` `aria-modal="true"` `aria-labelledby` |
| Tabs | arrow-key nav, roving `tabindex` | `role="tablist"` `tab` `tabpanel` `aria-selected` `aria-controls` |
| Accordion | `x-collapse` single/multi open | `:aria-expanded` `aria-controls` `role="region"` |
| Toasts | `Alpine.store('toasts')` `$nextTick` transition trigger | `aria-live="polite"` |
| Typeahead | `@input.debounce` arrow nav | `role="combobox"` `aria-autocomplete="list"` `role="listbox"`/`option` |
| Infinite scroll | `x-intersect.once="loadMore()"` | — |
| Form validation | getters for `errors`/`isValid`, `@blur` touched | `aria-invalid` `aria-describedby` |
| Drag-and-drop | `draggable="true"` `@dragstart` `@drop` | — |
