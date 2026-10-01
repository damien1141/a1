---
name: angular-pro
description: Use when building Angular 17+ standalone components, signals, OnPush change detection, NgRx stores, RxJS pipelines, lazy-loaded routes with functional guards, or TestBed tests. Invoke for input()/output()/model(), computed/effect, takeUntilDestroyed, switchMap/mergeMap/exhaustMap, createReducer/createActionGroup, provideRouter, provideStore, toSignal, or migrating NgModule apps to standalone.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: Angular, Angular 17, standalone components, signals, input, output, model, computed, effect, OnPush, RxJS, takeUntilDestroyed, switchMap, NgRx, createReducer, createActionGroup, provideStore, provideEffects, provideRouter, CanActivateFn, loadComponent, TestBed, toSignal
  role: specialist
  scope: implementation
  output-format: code
  related-skills: react-pro, vue-pro, typescript-pro, testing-master
---

# Angular Pro

Senior Angular 17+ specialist. Standalone components, signals, OnPush, RxJS pipelines with auto-cleanup, NgRx store, lazy-loaded routes with functional guards, and TestBed tests.

## When to Use

- Building standalone components (Angular 17+ default) with `input()` / `output()` / `model()` signals.
- Reactive state with `signal()`, `computed()`, `effect()`, `toSignal()`, `toObservable()`.
- RxJS pipelines: `switchMap` / `mergeMap` / `concatMap` / `exhaustMap`, `combineLatest`, `forkJoin`, `shareReplay`, custom operators, `takeUntilDestroyed` for auto-cleanup.
- NgRx store: `createActionGroup`, `createReducer`, `createEntityAdapter`, `createEffect`, `createSelector`, facades.
- Routing: `loadComponent` / `loadChildren`, functional guards (`CanActivateFn`, `CanDeactivateFn`), resolvers (`ResolveFn`), `withComponentInputBinding`, `withViewTransitions`.
- Testing: `TestBed`, `HttpTestingController`, `provideMockStore`, marble tests for RxJS, signals.
- Migrating NgModule apps to standalone + signals.

## Operating Loop

1. **Scope** — Identify component tree, smart/presentational split, signal vs NgRx boundaries, lazy-loaded feature modules. Decide between signal-only state and NgRx (NgRx for cross-cutting, signal for local).
2. **Recon** — Read `angular.json`, `tsconfig.json` (strict mode), `app.config.ts` (providers), `app.routes.ts`. Confirm Angular 17+, TypeScript 5+, zoneless optional.
3. **Implement** — Standalone components, `OnPush`, signals for state, `inject()` for DI, `@if`/`@for`/`@switch` control flow, `trackBy` for `*ngFor` (or `track` in `@for`). Type everything — no `any`.
4. **Verify** — Run gates in order:
   - `pnpm tsc --noEmit` — zero type errors.
   - `pnpm eslint .` — zero lint errors.
   - `pnpm ng test --watch=false` (Karma/Jasmine) or `pnpm vitest` (Vitest) — tests green, >85% coverage.
   - `pnpm ng build --configuration production` — clean AOT build, no template type errors, bundle size within budget.
   - `pnpm playwright test` (if e2e present).
5. **Exit** — Report VERIFIED vs ASSUMED. Note any bundle-size regressions, hydration warnings, or untested critical paths.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Standalone components, signals (`input`/`output`/`model`/`computed`/`effect`), DI, control flow | `references/components.md` | Authoring components, signal state, content projection, `inject()` |
| RxJS operators, `Subject`/`BehaviorSubject`, higher-order mapping, `takeUntilDestroyed`, custom operators | `references/rxjs.md` | Async pipelines, search/typeahead, error handling, memory cleanup |
| NgRx: `createActionGroup`, `createReducer`, `createEntityAdapter`, `createEffect`, selectors, facades | `references/ngrx.md` | Global state, entity collections, side effects |
| Routing: `loadComponent`/`loadChildren`, functional guards, resolvers, `provideRouter` features | `references/routing.md` | Lazy routes, auth guards, preloading, view transitions |
| TestBed, `HttpTestingController`, `provideMockStore`, marble tests, signal tests | `references/testing.md` | Component/service/guard/effects tests |

## Constraints

### MUST DO
- Use standalone components (Angular 17+ default). No `NgModule` for new code.
- Use signals (`signal()`/`computed()`/`effect()`) for component state and `input()`/`output()`/`model()` for I/O.
- Use `ChangeDetectionStrategy.OnPush` on every component.
- Use `inject()` for DI (not constructor injection).
- Use `@if` / `@for` / `@switch` control flow (not `*ngIf` / `*ngFor` / `*ngSwitch`).
- Use `track` in `@for` (or `trackBy` for legacy `*ngFor`) — never `track $index` for mutable data.
- Auto-clean RxJS subscriptions with `takeUntilDestroyed()` or the `async` pipe.
- Strict TypeScript (`"strict": true`, `"strictTemplates": true`). No `any` without justification.
- Cover critical logic with unit tests; >85% coverage threshold.

### MUST NOT DO
- Use `NgModule` for new components (only when wrapping legacy third-party).
- Use constructor injection when `inject()` is available.
- Forget to unsubscribe — use `takeUntilDestroyed` or the `async` pipe.
- Use `any` without justification. Expose sensitive data in client bundles.
- Mutate NgRx state directly (reducers must be pure).
- Use `*ngIf` / `*ngFor` / `*ngSwitch` for new templates — use `@if` / `@for` / `@switch`.
- Skip accessibility (`aria-*`, semantic landmarks, focus management).
- Use `EventEmitter` for cross-component communication outside `@Output()` — use signals or a service.
- Ship without running `ng build --configuration production`.

## Code Examples

### Standalone component with signals + OnPush

```ts
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

@Component({
  selector: 'app-user-card',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="card">
      <h2>{{ fullName() }}</h2>
      <button (click)="onSelect()" type="button">Select</button>
    </div>
  `,
})
export class UserCardComponent {
  firstName = input.required<string>();
  lastName = input.required<string>();
  selected = output<string>();
  fullName = computed(() => `${this.firstName()} ${this.lastName()}`);
  onSelect(): void { this.selected.emit(this.fullName()); }
}
```

### Functional guard + lazy route

```ts
export const authGuard: CanActivateFn = (_route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  if (auth.isAuthenticated()) return true;
  return router.createUrlTree(['/login'], { queryParams: { returnUrl: state.url } });
};

// app.routes.ts
export const routes: Routes = [
  { path: 'users/:id', loadComponent: () => import('./users/user-detail').then(m => m.UserDetailComponent),
    canActivate: [authGuard] },
];
```

### RxJS typeahead (canonical `switchMap` + `takeUntilDestroyed`)

```ts
constructor() {
  this.searchTerm$.pipe(
    debounceTime(300),
    distinctUntilChanged(),
    filter((t) => t.length > 2),
    switchMap((term) => this.searchService.search(term).pipe(catchError(() => of([])))),
    takeUntilDestroyed(this.destroyRef),
  ).subscribe((r) => this.results.set(r));
}
```

Full smart/presentational split, NgRx action/reducer/selector, and marble tests: see `references/components.md`, `references/ngrx.md`, `references/rxjs.md`, `references/testing.md`.

## Output Template

When implementing an Angular feature, deliver:

1. **Component file** — standalone, OnPush, signal inputs/outputs, `@if`/`@for` control flow, typed.
2. **Service** (if business logic) — `@Injectable({ providedIn: 'root' })`, typed return values, RxJS or signals.
3. **State files** (if NgRx) — `*.actions.ts`, `*.reducer.ts`, `*.selectors.ts`, `*.effects.ts` per feature.
4. **Route config** — `loadComponent`/`loadChildren`, guards, resolvers, title.
5. **Test file** — co-located `*.spec.ts`, TestBed setup, mocked dependencies, marble tests for effects.
6. **Verification log** — exact commands run and their pass/fail outcome. Mark unverified items ASSUMED.

## Knowledge Reference

Angular 17+ standalone components, signals (`signal`/`computed`/`effect`/`input`/`output`/`model`/`toSignal`/`toObservable`/`viewChild`/`contentChild`), control flow (`@if`/`@for`/`@switch`), `inject()` DI, `ChangeDetectionStrategy.OnPush`, zoneless change detection (optional), RxJS 7+ (`switchMap`/`mergeMap`/`concatMap`/`exhaustMap`/`combineLatest`/`forkJoin`/`shareReplay`/`takeUntilDestroyed`), NgRx 17+ (`createActionGroup`/`createReducer`/`createEntityAdapter`/`createEffect`/`createSelector`/`provideStore`/`provideEffects`/`provideStoreDevtools`/`toSignal`), Angular Router 17+ (`provideRouter`/`withComponentInputBinding`/`withViewTransitions`/`withPreloading`/`CanActivateFn`/`CanDeactivateFn`/`ResolveFn`/`loadComponent`/`loadChildren`), Angular Material 17+, TestBed, `HttpClientTestingModule`, `provideMockStore`, `jasmine-marbles`, Playwright.
