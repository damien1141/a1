# RxJS Patterns for Angular

## Essential operators

```ts
import {
  map, filter, switchMap, mergeMap, concatMap, exhaustMap,
  catchError, retry, debounceTime, distinctUntilChanged,
  tap, shareReplay, takeUntil, finalize,
} from 'rxjs/operators';
import { Subject, BehaviorSubject, ReplaySubject, AsyncSubject, of, throwError, timer, from, combineLatest, forkJoin, merge, zip } from 'rxjs';
```

## Typeahead / search (canonical pattern)

```ts
import { Component, inject, signal, DestroyRef } from '@angular/core';
import { Subject } from 'rxjs';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';

@Component({ selector: 'app-search', standalone: true })
export class SearchComponent {
  private searchService = inject(SearchService);
  private destroyRef = inject(DestroyRef);

  searchTerm$ = new Subject<string>();
  results = signal<SearchResult[]>([]);
  loading = signal(false);

  constructor() {
    this.searchTerm$.pipe(
      debounceTime(300),                  // wait for typing pause
      distinctUntilChanged(),             // skip if unchanged
      filter((t) => t.length > 2),        // min length
      tap(() => this.loading.set(true)),
      switchMap((term) =>                 // cancel previous, use latest
        this.searchService.search(term).pipe(
          catchError((err) => { console.error(err); return of([]); }),
        ),
      ),
      tap(() => this.loading.set(false)),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe((r) => this.results.set(r));
  }

  onInput(e: Event) {
    this.searchTerm$.next((e.target as HTMLInputElement).value);
  }
}
```

## Subject types

| Subject | Behavior |
|---------|----------|
| `Subject` | No initial value; only future emissions reach subscribers |
| `BehaviorSubject` | Has initial value; new subscribers get latest |
| `ReplaySubject<N>` | Replays last N values to new subscribers |
| `AsyncSubject` | Emits only the last value when completed |

```ts
const click$ = new Subject<MouseEvent>();
const loading$ = new BehaviorSubject<boolean>(false);
const recent$ = new ReplaySubject<Activity>(3);
const final$ = new AsyncSubject<Result>();
```

## Higher-order mapping operators

```ts
// switchMap: cancel previous, use latest (search, typeahead)
searchUsers(term$: Observable<string>) {
  return term$.pipe(switchMap((t) => this.http.get<User[]>(`/api/users?q=${t}`)));
}

// mergeMap: all concurrent (independent parallel requests)
uploadFiles(files: File[]) {
  return from(files).pipe(mergeMap((f) => this.http.post('/api/upload', f)));
}

// concatMap: sequential (order matters)
processQueue(tasks: Task[]) {
  return from(tasks).pipe(concatMap((t) => this.http.post('/api/process', t)));
}

// exhaustMap: ignore new until current completes (prevent double-submit)
saveForm(clicks$: Observable<void>, data: FormData) {
  return clicks$.pipe(exhaustMap(() => this.http.post('/api/save', data)));
}
```

## Combining observables

```ts
// combineLatest: emit when ANY source emits, latest values from all
loadDashboard() {
  return combineLatest({
    user: this.http.get<User>('/api/user'),
    stats: this.http.get<Stats>('/api/stats'),
    notifications: this.http.get<Notification[]>('/api/notifications'),
  }).pipe(map(({ user, stats, notifications }) => ({ user, stats, notifications })));
}

// forkJoin: emit when ALL complete (like Promise.all)
loadAllData() {
  return forkJoin({
    users: this.http.get<User[]>('/api/users'),
    products: this.http.get<Product[]>('/api/products'),
    orders: this.http.get<Order[]>('/api/orders'),
  });
}

// merge: emit when any source emits (flattens)
activityFeed() {
  return merge(this.http.get<Activity[]>('/api/recent'), this.http.get<Activity[]>('/api/trending'));
}

// zip: emit when all sources have emitted at the same index (pairing)
pairs() {
  return zip(this.http.get<User[]>('/api/users'), this.http.get<Profile[]>('/api/profiles'));
}
```

## Error handling

```ts
// Retry with exponential backoff
getData() {
  return this.http.get<Data>('/api/data').pipe(
    retry({ count: 3, delay: (err, i) => timer(Math.pow(2, i) * 1000) }),
    catchError((err) => {
      console.error('Failed after retries:', err);
      return of(null);
    }),
  );
}

// Re-throw with context
saveData(data: Data) {
  return this.http.post('/api/data', data).pipe(
    catchError((err) => err.status === 401
      ? throwError(() => new Error('Unauthorized'))
      : throwError(() => err)),
  );
}

// Finalize (always runs, success or error)
loadWithCleanup() {
  return this.http.get('/api/data').pipe(
    tap(() => this.loading.set(true)),
    finalize(() => this.loading.set(false)),
  );
}
```

## Memory management

### `takeUntilDestroyed` (preferred)

```ts
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Component, DestroyRef, inject } from '@angular/core';

@Component({ standalone: true })
export class AutoCleanupComponent {
  private dataService = inject(DataService);
  private destroyRef = inject(DestroyRef);
  data = signal<Data[]>([]);

  constructor() {
    // Auto-unsubscribe on component destroy
    this.dataService.getData()
      .pipe(takeUntilDestroyed())
      .subscribe((d) => this.data.set(d));

    // Or with explicit DestroyRef (for use outside constructor)
    const sub = this.dataService.getUpdates().subscribe();
    this.destroyRef.onDestroy(() => sub.unsubscribe());
  }
}
```

### `async` pipe (template auto-unsubscribe)

```ts
@Component({
  standalone: true,
  template: `
    @if (users$ | async; as users) {
      <ul><li *ngFor="let u of users">{{ u.name }}</li></ul>
    }
  `,
})
export class UsersComponent {
  private api = inject(UsersApiService);
  users$ = this.api.getAll().pipe(shareReplay({ bufferSize: 1, refCount: true }));
}
```

## `shareReplay` for caching

```ts
@Injectable({ providedIn: 'root' })
export class ConfigService {
  private http = inject(HttpClient);
  config$ = this.http.get<Config>('/api/config').pipe(
    shareReplay({ bufferSize: 1, refCount: true }),
  );
  getConfig() { return this.config$; }
}
```

## `toSignal` / `toObservable` (signals ↔ RxJS bridge)

```ts
import { toSignal, toObservable } from '@angular/core/rxjs-interop';

@Component({ standalone: true })
export class UsersComponent {
  private store = inject(Store);

  // Observable → Signal (for templates and OnPush)
  users = toSignal(this.store.select(selectAllUsers), { initialValue: [] as User[] });
  loading = toSignal(this.store.select(selectUsersLoading), { initialValue: false });

  // Signal → Observable (for RxJS pipelines)
  private filter = signal('');
  filter$ = toObservable(this.filter);

  constructor() {
    this.filter$.pipe(
      debounceTime(300),
      switchMap((f) => this.api.search(f)),
      takeUntilDestroyed(),
    ).subscribe((r) => this.results.set(r));
  }
}
```

## Custom operators

```ts
import { Observable, OperatorFunction } from 'rxjs';
import { tap } from 'rxjs/operators';

export function debug<T>(tag: string): OperatorFunction<T, T> {
  return (source: Observable<T>) => source.pipe(
    tap({
      next: (v) => console.log(`[${tag}] Next:`, v),
      error: (e) => console.error(`[${tag}] Error:`, e),
      complete: () => console.log(`[${tag}] Complete`),
    }),
  );
}

// Usage
this.http.get('/api/data').pipe(debug('API'), map(transform)).subscribe();
```

## Quick Reference

| Use case | Operator |
|----------|----------|
| Transform | `map` |
| Filter | `filter`, `distinctUntilChanged` |
| Time | `debounceTime`, `throttleTime`, `delay` |
| Cancel previous | `switchMap` |
| All concurrent | `mergeMap` |
| Sequential | `concatMap` |
| Ignore new | `exhaustMap` |
| Latest from all | `combineLatest` |
| Wait for all | `forkJoin` |
| Flatten | `merge` |
| Pair | `zip` |
| Error | `catchError`, `retry` |
| Cleanup | `takeUntilDestroyed`, `async` pipe |
| Share result | `shareReplay` |
| Side effect | `tap`, `finalize` |
| Observable → Signal | `toSignal(obs, { initialValue })` |
| Signal → Observable | `toObservable(signal)` |
