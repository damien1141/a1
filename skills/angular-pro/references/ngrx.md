# NgRx State Management

## Store setup

```ts
// app.config.ts
import { ApplicationConfig, isDevMode } from '@angular/core';
import { provideStore } from '@ngrx/store';
import { provideEffects } from '@ngrx/effects';
import { provideStoreDevtools } from '@ngrx/store-devtools';
import { provideRouter } from '@angular/router';
import { routes } from './app.routes';
import { usersReducer } from './users/users.reducer';
import { UsersEffects } from './users/users.effects';

export const appConfig: ApplicationConfig = {
  providers: [
    provideRouter(routes),
    provideStore({ users: usersReducer }),
    provideEffects([UsersEffects]),
    provideStoreDevtools({ maxAge: 25, logOnly: !isDevMode() }),
  ],
};
```

## Actions (`createActionGroup`)

```ts
// users.actions.ts
import { createActionGroup, emptyProps, props } from '@ngrx/store';
import { User } from './user.model';

export const UsersActions = createActionGroup({
  source: 'Users',
  events: {
    'Load Users': emptyProps(),
    'Load Users Success': props<{ users: User[] }>(),
    'Load Users Failure': props<{ error: string }>(),
    'Add User': props<{ user: User }>(),
    'Add User Success': props<{ user: User }>(),
    'Update User': props<{ id: string; changes: Partial<User> }>(),
    'Delete User': props<{ id: string }>(),
    'Delete User Success': props<{ id: string }>(),
  },
});
```

## Reducer with entity adapter

```ts
// users.reducer.ts
import { createReducer, on } from '@ngrx/store';
import { createEntityAdapter, EntityState } from '@ngrx/entity';
import { UsersActions } from './users.actions';
import { User } from './user.model';

export interface UsersState extends EntityState<User> {
  loading: boolean;
  error: string | null;
  selectedUserId: string | null;
}

export const usersAdapter = createEntityAdapter<User>({
  selectId: (u) => u.id,
  sortComparer: (a, b) => a.name.localeCompare(b.name),
});

const initialState: UsersState = usersAdapter.getInitialState({
  loading: false,
  error: null,
  selectedUserId: null,
});

export const usersReducer = createReducer(initialState,
  on(UsersActions.loadUsers, (s) => ({ ...s, loading: true, error: null })),
  on(UsersActions.loadUsersSuccess, (s, { users }) =>
    usersAdapter.setAll(users, { ...s, loading: false })),
  on(UsersActions.loadUsersFailure, (s, { error }) => ({ ...s, loading: false, error })),
  on(UsersActions.addUserSuccess, (s, { user }) => usersAdapter.addOne(user, s)),
  on(UsersActions.updateUser, (s, { id, changes }) =>
    usersAdapter.updateOne({ id, changes }, s)),
  on(UsersActions.deleteUserSuccess, (s, { id }) => usersAdapter.removeOne(id, s)),
);
```

## Selectors

```ts
// users.selectors.ts
import { createFeatureSelector, createSelector } from '@ngrx/store';
import { usersAdapter, UsersState } from './users.reducer';

export const selectUsersState = createFeatureSelector<UsersState>('users');
const { selectIds, selectEntities, selectAll, selectTotal } = usersAdapter.getSelectors();

export const selectUserIds = createSelector(selectUsersState, selectIds);
export const selectUserEntities = createSelector(selectUsersState, selectEntities);
export const selectAllUsers = createSelector(selectUsersState, selectAll);
export const selectUsersTotal = createSelector(selectUsersState, selectTotal);
export const selectUsersLoading = createSelector(selectUsersState, (s) => s.loading);
export const selectUsersError = createSelector(selectUsersState, (s) => s.error);

// Parameterized selector
export const selectUserById = (id: string) =>
  createSelector(selectUserEntities, (entities) => entities[id]);

// Composed
export const selectActiveUsers = createSelector(
  selectAllUsers,
  (users) => users.filter((u) => u.isActive),
);
```

## Effects

```ts
// users.effects.ts
import { Injectable, inject } from '@angular/core';
import { Actions, createEffect, ofType } from '@ngrx/effects';
import { catchError, exhaustMap, map, mergeMap, tap } from 'rxjs/operators';
import { of } from 'rxjs';
import { UsersService } from './users.service';
import { UsersActions } from './users.actions';

@Injectable()
export class UsersEffects {
  private actions$ = inject(Actions);
  private usersService = inject(UsersService);

  loadUsers$ = createEffect(() =>
    this.actions$.pipe(
      ofType(UsersActions.loadUsers),
      mergeMap(() =>
        this.usersService.getAll().pipe(
          map((users) => UsersActions.loadUsersSuccess({ users })),
          catchError((error) => of(UsersActions.loadUsersFailure({ error: error.message }))),
        ),
      ),
    ),
  );

  // exhaustMap prevents duplicate submits
  addUser$ = createEffect(() =>
    this.actions$.pipe(
      ofType(UsersActions.addUser),
      exhaustMap(({ user }) =>
        this.usersService.create(user).pipe(
          map((created) => UsersActions.addUserSuccess({ user: created })),
          catchError((error) => of(UsersActions.loadUsersFailure({ error: error.message }))),
        ),
      ),
    ),
  );

  // Non-dispatching side effect
  logUserActions$ = createEffect(
    () =>
      this.actions$.pipe(
        ofType(UsersActions.addUserSuccess, UsersActions.deleteUserSuccess),
        tap((action) => console.log('User action:', action)),
      ),
    { dispatch: false },
  );
}
```

## Component integration (signal-based)

```ts
// users-list.component.ts
import { Component, inject } from '@angular/core';
import { Store } from '@ngrx/store';
import { toSignal } from '@angular/core/rxjs-interop';
import { UsersActions } from './store/users.actions';
import { selectAllUsers, selectUsersLoading, selectUsersError } from './store/users.selectors';

@Component({
  selector: 'app-users-list',
  standalone: true,
  template: `
    @if (loading()) { <div>Loading…</div> }
    @else if (error(); as err) { <div>Error: {{ err }}</div> }
    @else {
      @for (u of users(); track u.id) {
        <div>{{ u.name }} <button (click)="onDelete(u.id)">Delete</button></div>
      }
    }
  `,
})
export class UsersListComponent {
  private store = inject(Store);

  users = toSignal(this.store.select(selectAllUsers), { initialValue: [] as User[] });
  loading = toSignal(this.store.select(selectUsersLoading), { initialValue: false });
  error = toSignal(this.store.select(selectUsersError), { initialValue: null as string | null });

  ngOnInit() { this.store.dispatch(UsersActions.loadUsers()); }
  onDelete(id: string) { this.store.dispatch(UsersActions.deleteUser({ id })); }
}
```

## Facade pattern (encapsulate store access)

```ts
@Injectable({ providedIn: 'root' })
export class UsersFacade {
  private store = inject(Store);
  users$ = this.store.select(selectAllUsers);
  loading$ = this.store.select(selectUsersLoading);
  error$ = this.store.select(selectUsersError);

  loadUsers() { this.store.dispatch(UsersActions.loadUsers()); }
  addUser(user: User) { this.store.dispatch(UsersActions.addUser({ user })); }
  deleteUser(id: string) { this.store.dispatch(UsersActions.deleteUser({ id })); }
  getUserById(id: string) { return this.store.select(selectUserById(id)); }
}

// Usage
@Component({ standalone: true })
export class UsersComponent {
  private facade = inject(UsersFacade);
  users = toSignal(this.facade.users$, { initialValue: [] as User[] });
  loading = toSignal(this.facade.loading$, { initialValue: false });
  ngOnInit() { this.facade.loadUsers(); }
}
```

## When to use NgRx vs signals

| Use case | Reach for |
|----------|-----------|
| Local component state | signals |
| Cross-component but feature-scoped | signals + shared service, or signals in a feature store |
| App-wide entity collections with many consumers | NgRx + entity adapter |
| Need time-travel devtools | NgRx |
| Need to coordinate effects across features | NgRx Effects |
| Hydration from server | NgRx + `provideStoreEntity` or signals + `toSignal` |

**Default**: signals for component state. Reach for NgRx when ≥3 unrelated components need the same mutating collection and you want a single source of truth with devtools.

## Quick Reference

| Concept | API |
|---------|-----|
| Action group | `createActionGroup({ source, events: { ... } })` |
| Action with payload | `props<{ users: User[] }>()` |
| Reducer | `createReducer(state, on(action, (s, p) => ...))` |
| Entity adapter | `createEntityAdapter<T>({ selectId, sortComparer })` |
| Selector | `createSelector(feature, projector)` |
| Feature selector | `createFeatureSelector<State>('name')` |
| Effect | `createEffect(() => actions$.pipe(ofType(...), map(...)))` |
| Side-effect (no dispatch) | `createEffect(fn, { dispatch: false })` |
| Provider | `provideStore({ key: reducer })`, `provideEffects([Effects])` |
| DevTools | `provideStoreDevtools({ maxAge, logOnly })` |
| Component read | `toSignal(store.select(selector), { initialValue })` |
| Component dispatch | `store.dispatch(Actions.x({ payload }))` |
