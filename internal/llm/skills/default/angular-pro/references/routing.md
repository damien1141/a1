# Angular Routing: lazy loading, guards, resolvers, preloading

## Routes configuration

```ts
// app.routes.ts
import { Routes } from '@angular/router';
import { HomeComponent } from './home/home.component';
import { authGuard } from './guards/auth.guard';
import { adminGuard } from './guards/admin.guard';
import { userResolver } from './resolvers/user.resolver';
import { canDeactivateGuard } from './guards/can-deactivate.guard';

export const routes: Routes = [
  { path: '', redirectTo: '/home', pathMatch: 'full' },
  { path: 'home', component: HomeComponent, title: 'Home' },

  // Lazy-loaded standalone component
  {
    path: 'users',
    loadComponent: () => import('./users/users.component').then((m) => m.UsersComponent),
    title: 'Users',
  },

  // Dynamic param + guard + resolver
  {
    path: 'users/:id',
    loadComponent: () => import('./users/user-detail.component').then((m) => m.UserDetailComponent),
    canActivate: [authGuard],
    resolve: { user: userResolver },
  },

  // Lazy-loaded child routes
  {
    path: 'admin',
    loadChildren: () => import('./admin/admin.routes').then((m) => m.ADMIN_ROUTES),
    canActivate: [authGuard, adminGuard],
  },

  // Wildcard
  {
    path: '**',
    loadComponent: () => import('./not-found/not-found.component').then((m) => m.NotFoundComponent),
    title: '404',
  },
];
```

```ts
// app.config.ts
import { ApplicationConfig, provideZoneChangeDetection } from '@angular/core';
import { provideRouter, withComponentInputBinding, withViewTransitions, withPreloading, PreloadAllModules } from '@angular/router';
import { routes } from './app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    provideZoneChangeDetection('eventcoop'),
    provideRouter(
      routes,
      withComponentInputBinding(),    // route params auto-bound to @Input()
      withViewTransitions(),          // View Transitions API
      withPreloading(PreloadAllModules),
    ),
  ],
};
```

## Lazy-loaded child routes

```ts
// admin/admin.routes.ts
import { Routes } from '@angular/router';

export const ADMIN_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () => import('./admin-dashboard.component').then((m) => m.AdminDashboardComponent),
  },
  {
    path: 'users',
    loadComponent: () => import('./admin-users.component').then((m) => m.AdminUsersComponent),
  },
  {
    path: 'settings',
    loadComponent: () => import('./admin-settings.component').then((m) => m.AdminSettingsComponent),
  },
];
```

## Functional guards

```ts
// guards/auth.guard.ts
import { inject } from '@angular/core';
import { CanActivateFn, CanDeactivateFn, CanMatchFn, Router, RouterStateSnapshot } from '@angular/router';
import { AuthService } from '../services/auth.service';
import { Observable, of } from 'rxjs';
import { map, catchError } from 'rxjs/operators';

export const authGuard: CanActivateFn = (_route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  if (auth.isAuthenticated()) return true;
  return router.createUrlTree(['/login'], { queryParams: { returnUrl: state.url } });
};

export const adminGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  const router = inject(Router);
  return auth.hasRole('admin') ? true : router.createUrlTree(['/unauthorized']);
};

// Observable guard (async check)
export const dataGuard: CanActivateFn = (route) => {
  const dataService = inject(DataService);
  const router = inject(Router);
  return dataService.checkAccess(route.params['id']).pipe(
    map((ok) => ok ? true : router.createUrlTree(['/no-access'])),
    catchError(() => of(router.createUrlTree(['/error']))),
  );
};

// CanDeactivate — prompt before leaving with unsaved changes
export const canDeactivateGuard: CanDeactivateFn<FormComponent> = (component) => {
  if (component.hasUnsavedChanges()) {
    return confirm('You have unsaved changes. Leave?');
  }
  return true;
};

// CanMatch — decide whether a lazy route should be loaded
export const loadAdminGuard: CanMatchFn = () => {
  return inject(AuthService).hasRole('admin') ? true : inject(Router).createUrlTree(['/unauthorized']);
};
```

## Resolvers

```ts
// resolvers/user.resolver.ts
import { inject } from '@angular/core';
import { ResolveFn } from '@angular/router';
import { catchError, of } from 'rxjs';
import { UsersService } from '../services/users.service';
import { User } from '../models/user.model';

export const userResolver: ResolveFn<User | null> = (route) => {
  const usersService = inject(UsersService);
  const id = route.paramMap.get('id');
  if (!id) return of(null);
  return usersService.getById(id).pipe(catchError(() => of(null)));
};
```

```ts
@Component({
  standalone: true,
  template: `
    @if (user()) { <h1>{{ user()!.name }}</h1> }
    @else { <p>User not found</p> }
  `,
})
export class UserDetailComponent {
  // Resolved data auto-bound as signal input (withComponentInputBinding)
  user = input<User | null>(null);
}
```

## Programmatic navigation

```ts
import { Component, inject } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

@Component({ standalone: true })
export class ProductDetailComponent {
  private route = inject(ActivatedRoute);
  private router = inject(Router);

  id = input.required<string>();   // withComponentInputBinding

  goToEdit() {
    this.router.navigate(['/products', this.id(), 'edit']);
  }

  applyFilter(filter: string) {
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { filter },
      queryParamsHandling: 'merge',
    });
  }
}
```

Template-based:
```html
<a [routerLink]="['/products', id(), 'edit']">Edit</a>
<a routerLink="/products" [queryParams]="{ filter: 'active' }">Active</a>
```

## Router events

```ts
import { Component, inject } from '@angular/core';
import { NavigationStart, NavigationEnd, NavigationError, Router } from '@angular/router';
import { filter } from 'rxjs/operators';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';

@Component({ standalone: true })
export class AppComponent {
  private router = inject(Router);
  loading = signal(false);

  constructor() {
    this.router.events.pipe(takeUntilDestroyed()).subscribe((e) => {
      if (e instanceof NavigationStart) this.loading.set(true);
      if (e instanceof NavigationEnd || e instanceof NavigationError) this.loading.set(false);
    });
  }
}
```

## Child routes & named outlets

```ts
const routes: Routes = [
  {
    path: 'dashboard',
    component: DashboardComponent,
    children: [
      { path: 'stats', component: StatsComponent, outlet: 'panel' },
      { path: 'charts', component: ChartsComponent, outlet: 'panel' },
    ],
  },
];
```

```html
<div class="dashboard">
  <div class="main"><router-outlet /></div>
  <div class="panel"><router-outlet name="panel" /></div>
</div>
```

```ts
this.router.navigate(['/dashboard', { outlets: { panel: ['stats'] } }]);
```

## Custom preloading strategy

```ts
import { Injectable } from '@angular/core';
import { PreloadingStrategy, Route } from '@angular/router';
import { Observable, of, timer } from 'rxjs';
import { mergeMap } from 'rxjs/operators';

@Injectable({ providedIn: 'root' })
export class CustomPreloadingStrategy implements PreloadingStrategy {
  preload(route: Route, load: () => Observable<unknown>): Observable<unknown> {
    if (route.data?.['preload']) {
      const delay = route.data?.['preloadDelay'] ?? 0;
      return timer(delay).pipe(mergeMap(() => load()));
    }
    return of(null);
  }
}

// Routes
const routes: Routes = [
  {
    path: 'important',
    loadChildren: () => import('./important/important.routes'),
    data: { preload: true, preloadDelay: 2000 },
  },
];

// app.config.ts
provideRouter(routes, withPreloading(CustomPreloadingStrategy));
```

## Quick Reference

| Feature | API |
|---------|-----|
| Routes | `Routes` array in `app.routes.ts` |
| Lazy component | `loadComponent: () => import(...).then(m => m.X)` |
| Lazy children | `loadChildren: () => import(...).then(m => m.ROUTES)` |
| Guard (sync) | `CanActivateFn` returns `boolean \| UrlTree` |
| Guard (async) | returns `Observable<boolean \| UrlTree>` |
| CanDeactivate | `CanDeactivateFn<Component>` |
| CanMatch | `CanMatchFn` |
| Resolver | `ResolveFn<T>` |
| Params | `route.paramMap` or `input<T>()` (with `withComponentInputBinding`) |
| Query | `route.queryParamMap` |
| Navigate | `router.navigate([...])`, `routerLink` |
| Events | `router.events` |
| Named outlet | `<router-outlet name="...">`, `outlets: { name: [...] }` |
| Preload | `withPreloading(strategy)` |
| View transitions | `withViewTransitions()` |
| Input binding | `withComponentInputBinding()` |
