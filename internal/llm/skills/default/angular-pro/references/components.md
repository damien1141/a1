# Standalone Components, Signals, DI, Control Flow

## Standalone component (default since Angular 17)

```ts
import { ChangeDetectionStrategy, Component, signal, computed, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';

@Component({
  selector: 'app-user-profile',
  standalone: true,
  imports: [CommonModule, FormsModule],          // import only what you use
  templateUrl: './user-profile.component.html',
  styleUrl: './user-profile.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class UserProfileComponent {
  count = signal(0);
  double = computed(() => this.count() * 2);

  constructor() {
    effect(() => console.log(`count: ${this.count()}`));
  }

  increment() { this.count.update((v) => v + 1); }
}
```

## Signal inputs / outputs / model

```ts
import { Component, input, output, model } from '@angular/core';

@Component({
  selector: 'app-search-box',
  standalone: true,
  template: `
    <input [value]="query()" (input)="onInput($event)" [placeholder]="placeholder()" />
  `,
})
export class SearchBoxComponent {
  // Signal inputs (Angular 17.1+)
  placeholder = input<string>('Search…');
  // Two-way binding with model signal
  query = model<string>('');                       // supports [(query)]="..."
  // Signal outputs
  queryChange = output<string>();

  onInput(e: Event) {
    const v = (e.target as HTMLInputElement).value;
    this.query.set(v);
    this.queryChange.emit(v);
  }
}

// Parent usage
@Component({
  standalone: true,
  imports: [SearchBoxComponent],
  template: `
    <app-search-box [(query)]="searchQuery" [placeholder]="'Find users…'" (queryChange)="onSearch($event)" />
  `,
})
export class ParentComponent {
  searchQuery = signal('');
  onSearch(q: string) { console.log('Searching:', q); }
}
```

## Smart vs Presentational

```ts
// Smart — owns state, calls services
@Component({
  selector: 'app-users-container',
  standalone: true,
  imports: [UserListComponent],
  template: `
    @if (loading()) { <div>Loading…</div> }
    @else { <app-user-list [users]="users()" (userSelected)="onSelect($event)" /> }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class UsersContainerComponent {
  private api = inject(UsersApiService);
  users = signal<User[]>([]);
  loading = signal(true);

  constructor() {
    this.api.getAll()
      .pipe(takeUntilDestroyed())
      .subscribe({
        next: (u) => { this.users.set(u); this.loading.set(false); },
        error: (e) => console.error(e),
      });
  }
  onSelect(user: User) { /* navigate */ }
}

// Presentational — only inputs/outputs, no service dependencies
@Component({
  selector: 'app-user-list',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @for (u of users(); track u.id) {
      <button type="button" (click)="userSelected.emit(u)">{{ u.name }}</button>
    } @empty { <p>No users.</p> }
  `,
})
export class UserListComponent {
  users = input.required<User[]>();
  userSelected = output<User>();
}
```

## New control flow (`@if` / `@for` / `@switch`)

```ts
@Component({
  standalone: true,
  template: `
    @if (user(); as currentUser) {
      <div>Hello, {{ currentUser.name }}</div>
    } @else if (loading()) {
      <div>Loading…</div>
    } @else {
      <div>Please log in</div>
    }

    @for (item of items(); track item.id) {
      <div>{{ item.name }}</div>
    } @empty {
      <div>No items</div>
    }

    @switch (status()) {
      @case ('pending') { <span>Pending…</span> }
      @case ('success') { <span>OK</span> }
      @default { <span>Unknown</span> }
    }
  `,
})
export class ModernControlFlowComponent {
  user = signal<User | null>(null);
  loading = signal(false);
  items = signal<Item[]>([]);
  status = signal<'pending' | 'success' | 'error'>('pending');
}
```

`track` is required on `@for` — use a stable ID, never `$index` for mutable data.

## Content projection (slots)

```ts
@Component({
  selector: 'app-card',
  standalone: true,
  template: `
    <div class="card">
      <div class="card-header"><ng-content select="[header]" /></div>
      <div class="card-body"><ng-content /></div>
      <div class="card-footer"><ng-content select="[footer]" /></div>
    </div>
  `,
})
export class CardComponent {}

// Usage
@Component({
  standalone: true,
  imports: [CardComponent],
  template: `
    <app-card>
      <h2 header>Title</h2>
      <p>Body</p>
      <button footer>Action</button>
    </app-card>
  `,
})
export class ParentComponent {}
```

## Dependency injection (`inject()`)

```ts
import { Component, inject, Injectable, PLATFORM_ID } from '@angular/core';

@Injectable({ providedIn: 'root' })
export class UsersService {
  private http = inject(HttpClient);
  getAll() { return this.http.get<User[]>('/api/users'); }
}

@Component({ standalone: true })
export class UserDashboardComponent {
  private usersService = inject(UsersService);
  private router = inject(Router);
  // Optional deps
  private logger = inject(LoggerService, { optional: true });
  // Platform token
  private platformId = inject(PLATFORM_ID);

  load() {
    this.usersService.getAll().subscribe({
      next: (u) => this.users.set(u),
      error: (e) => this.logger?.error('Failed', e),
    });
  }
}
```

## `viewChild` / `contentChild` signals

```ts
import { Component, viewChild, contentChild, ElementRef } from '@angular/core';

@Component({ standalone: true, template: `<input #input />` })
export class FormComponent {
  inputRef = viewChild<ElementRef<HTMLInputElement>>('input');

  ngAfterViewInit() {
    this.inputRef()?.nativeElement.focus();   // signal that updates on view init
  }
}
```

## Performance: OnPush + `track`

```ts
@Component({
  selector: 'app-product-list',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @for (p of products(); track p.id) {
      <app-product-card [product]="p" />
    }
  `,
})
export class ProductListComponent {
  products = input.required<Product[]>();
}
```

## Zoneless (Angular 18+)

```ts
// app.config.ts
export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),   // signals drive CD instead of Zone.js
    provideRouter(routes),
  ],
};
```

With zoneless, signals must drive all UI state. RxJS observables need `toSignal()` to be visible to CD.

## Quick Reference

| Pattern | Modern API |
|---------|-----------|
| Component | `standalone: true` (default in 17+) |
| State | `signal()`, `computed()`, `effect()` |
| Input | `input()`, `input.required()` |
| Output | `output<T>()` |
| Two-way | `model<T>()` |
| DI | `inject()` |
| View child | `viewChild()` signal |
| Content child | `contentChild()` signal |
| Control flow | `@if`, `@for`, `@switch` |
| Change detection | `ChangeDetectionStrategy.OnPush` (or zoneless) |
| Lifecycle | `ngOnInit`, `ngOnDestroy`, `ngAfterViewInit`, etc. (still valid; prefer effects when possible) |
