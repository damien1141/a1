# Angular Testing: TestBed, HttpTestingController, MockStore, marbles

## Component test

```ts
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { UserListComponent } from './user-list.component';
import { UsersService } from './users.service';
import { of } from 'rxjs';
import { User } from './user.model';

describe('UserListComponent', () => {
  let fixture: ComponentFixture<UserListComponent>;
  let component: UserListComponent;
  let usersService: jasmine.SpyObj<UsersService>;

  const mockUsers: User[] = [
    { id: '1', name: 'John Doe', email: 'john@e.com' },
    { id: '2', name: 'Jane Smith', email: 'jane@e.com' },
  ];

  beforeEach(async () => {
    const spy = jasmine.createSpyObj<UsersService>('UsersService', ['getAll', 'delete']);

    await TestBed.configureTestingModule({
      imports: [UserListComponent],                  // standalone
      providers: [
        { provide: UsersService, useValue: spy },
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();

    usersService = TestBed.inject(UsersService) as jasmine.SpyObj<UsersService>;
    fixture = TestBed.createComponent(UserListComponent);
    component = fixture.componentInstance;
  });

  it('creates', () => {
    usersService.getAll.and.returnValue(of(mockUsers));
    fixture.detectChanges();   // triggers ngOnInit + signal update
    expect(component).toBeTruthy();
  });

  it('renders users', () => {
    component.users.set(mockUsers);
    fixture.detectChanges();
    const items = fixture.nativeElement.querySelectorAll('.user-item');
    expect(items.length).toBe(2);
    expect(items[0].textContent).toContain('John Doe');
  });

  it('dispatches delete', () => {
    component.onDelete('1');
    expect(usersService.delete).toHaveBeenCalledWith('1');
  });
});
```

## Service test with HttpTestingController

```ts
import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { UsersService } from './users.service';
import { User } from './user.model';

describe('UsersService', () => {
  let service: UsersService;
  let httpMock: HttpTestingController;

  const mockUsers: User[] = [
    { id: '1', name: 'John', email: 'john@e.com' },
    { id: '2', name: 'Jane', email: 'jane@e.com' },
  ];

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [UsersService, provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(UsersService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());   // no outstanding requests

  it('GET /api/users', (done) => {
    service.getAll().subscribe((users) => {
      expect(users).toEqual(mockUsers);
      done();
    });
    const req = httpMock.expectOne('/api/users');
    expect(req.request.method).toBe('GET');
    req.flush(mockUsers);
  });

  it('POST creates a user', (done) => {
    const newUser: User = { id: '3', name: 'Bob', email: 'bob@e.com' };
    service.create(newUser).subscribe((u) => {
      expect(u).toEqual(newUser);
      done();
    });
    const req = httpMock.expectOne('/api/users');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual(newUser);
    req.flush(newUser);
  });

  it('handles 500', (done) => {
    service.getAll().subscribe({
      next: () => fail('should have errored'),
      error: (e) => { expect(e.status).toBe(500); done(); },
    });
    const req = httpMock.expectOne('/api/users');
    req.flush('Server error', { status: 500, statusText: 'Internal Server Error' });
  });
});
```

## RxJS marble testing

```ts
import { TestScheduler } from 'rxjs/testing';
import { delay, map } from 'rxjs/operators';

describe('RxJS operators', () => {
  let scheduler: TestScheduler;

  beforeEach(() => {
    scheduler = new TestScheduler((actual, expected) => expect(actual).toEqual(expected));
  });

  it('maps values', () => {
    scheduler.run(({ cold, expectObservable }) => {
      const source = cold('--a--b--c--|', { a: 1, b: 2, c: 3 });
      const expected = '    --x--y--z--|';
      const result = source.pipe(map((x) => x * 10));
      expectObservable(result).toBe(expected, { x: 10, y: 20, z: 30 });
    });
  });

  it('delays emissions', () => {
    scheduler.run(({ cold, expectObservable }) => {
      const source = cold('--a--b--|', { a: 1, b: 2 });
      const expected = '    ----a--b--|';
      expectObservable(source.pipe(delay(20))).toBe(expected, { a: 1, b: 2 });
    });
  });
});
```

## Testing signals

```ts
import { signal, computed, effect } from '@angular/core';

describe('Signals', () => {
  it('updates with set/update', () => {
    const count = signal(0);
    expect(count()).toBe(0);
    count.set(5);
    expect(count()).toBe(5);
    count.update((v) => v + 1);
    expect(count()).toBe(6);
  });

  it('computed re-evaluates on dep change', () => {
    const n = signal(5);
    const doubled = computed(() => n() * 2);
    expect(doubled()).toBe(10);
    n.set(10);
    expect(doubled()).toBe(20);
  });

  it('effect runs on changes', () => {
    const log: number[] = [];
    const n = signal(0);
    effect(() => log.push(n()));   // effect must run inside injection context
    // In a real test use TestBed.runInInjectionContext
    n.set(1); n.set(2);
    expect(log).toEqual([0, 1, 2]);
  });
});
```

## Testing NgRx

### Store + selectors

```ts
import { TestBed } from '@angular/core/testing';
import { provideMockStore, MockStore } from '@ngrx/store/testing';
import { UsersComponent } from './users.component';
import { UsersActions } from './store/users.actions';
import { selectAllUsers, selectUsersLoading } from './store/users.selectors';

describe('UsersComponent with NgRx', () => {
  let fixture: ComponentFixture<UsersComponent>;
  let store: MockStore;

  const initialState = {
    users: {
      ids: ['1', '2'],
      entities: { '1': { id: '1', name: 'John' }, '2': { id: '2', name: 'Jane' } },
      loading: false,
    },
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [UsersComponent],
      providers: [provideMockStore({ initialState })],
    }).compileComponents();

    store = TestBed.inject(MockStore);
    fixture = TestBed.createComponent(UsersComponent);
  });

  it('selects users', () => {
    store.overrideSelector(selectAllUsers, [{ id: '1', name: 'John' }, { id: '2', name: 'Jane' }]);
    store.overrideSelector(selectUsersLoading, false);
    store.refreshState();
    fixture.detectChanges();
    expect(fixture.componentInstance.users().length).toBe(2);
  });

  it('dispatches delete', () => {
    const dispatchSpy = spyOn(store, 'dispatch');
    fixture.componentInstance.onDelete('1');
    expect(dispatchSpy).toHaveBeenCalledWith(UsersActions.deleteUser({ id: '1' }));
  });
});
```

### Effects with marbles

```ts
import { TestBed } from '@angular/core/testing';
import { provideMockActions } from '@ngrx/effects/testing';
import { Observable, of, throwError } from 'rxjs';
import { hot, cold } from 'jasmine-marbles';
import { UsersEffects } from './users.effects';
import { UsersService } from './users.service';
import { UsersActions } from './users.actions';

describe('UsersEffects', () => {
  let actions$: Observable<unknown>;
  let effects: UsersEffects;
  let usersService: jasmine.SpyObj<UsersService>;

  beforeEach(() => {
    const spy = jasmine.createSpyObj<UsersService>('UsersService', ['getAll']);
    TestBed.configureTestingModule({
      providers: [
        UsersEffects,
        provideMockActions(() => actions$),
        { provide: UsersService, useValue: spy },
      ],
    });
    effects = TestBed.inject(UsersEffects);
    usersService = TestBed.inject(UsersService) as jasmine.SpyObj<UsersService>;
  });

  it('loads users', () => {
    const users = [{ id: '1', name: 'John' }];
    const action = UsersActions.loadUsers();
    const outcome = UsersActions.loadUsersSuccess({ users });
    actions$ = hot('-a', { a: action });
    const response = cold('-b|', { b: users });
    const expected = cold('--c', { c: outcome });
    usersService.getAll.and.returnValue(response);
    expect(effects.loadUsers$).toBeObservable(expected);
  });

  it('handles error', () => {
    const action = UsersActions.loadUsers();
    const error = new Error('Failed');
    const outcome = UsersActions.loadUsersFailure({ error: error.message });
    actions$ = hot('-a', { a: action });
    const response = cold('-#|', {}, error);
    const expected = cold('--c', { c: outcome });
    usersService.getAll.and.returnValue(response);
    expect(effects.loadUsers$).toBeObservable(expected);
  });
});
```

## Testing functional guards

```ts
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { authGuard } from './auth.guard';
import { AuthService } from './auth.service';

describe('authGuard', () => {
  let auth: jasmine.SpyObj<AuthService>;
  let router: jasmine.SpyObj<Router>;

  beforeEach(() => {
    const authSpy = jasmine.createSpyObj<AuthService>('AuthService', ['isAuthenticated']);
    const routerSpy = jasmine.createSpyObj<Router>('Router', ['createUrlTree']);
    TestBed.configureTestingModule({
      providers: [
        { provide: AuthService, useValue: authSpy },
        { provide: Router, useValue: routerSpy },
      ],
    });
    auth = TestBed.inject(AuthService) as jasmine.SpyObj<AuthService>;
    router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
  });

  it('allows when authenticated', () => {
    auth.isAuthenticated.and.returnValue(true);
    const result = TestBed.runInInjectionContext(() => authGuard({} as any, {} as any));
    expect(result).toBe(true);
  });

  it('redirects when not authenticated', () => {
    auth.isAuthenticated.and.returnValue(false);
    const urlTree = {} as any;
    router.createUrlTree.and.returnValue(urlTree);
    const result = TestBed.runInInjectionContext(() =>
      authGuard({} as any, { url: '/protected' } as any),
    );
    expect(result).toBe(urlTree);
    expect(router.createUrlTree).toHaveBeenCalledWith(['/login'], { queryParams: { returnUrl: '/protected' } });
  });
});
```

## Verification gates

```bash
pnpm tsc --noEmit                            # type-check
pnpm eslint .                                # lint
pnpm ng test --watch=false                   # unit/component tests (Karma + Jasmine)
# or with Vitest
pnpm vitest run
pnpm ng build --configuration production     # AOT build, bundle size check
pnpm playwright test                         # e2e (if present)
```

## Quick Reference

| Test type | Key tools |
|-----------|-----------|
| Component | `TestBed`, `ComponentFixture`, `detectChanges()` |
| Service | `provideHttpClient()`, `provideHttpClientTesting()`, `HttpTestingController` |
| RxJS | `TestScheduler`, `cold`/`hot` marble diagrams |
| NgRx Store | `provideMockStore({ initialState })`, `MockStore`, `overrideSelector` |
| Effects | `provideMockActions(() => actions$)`, `jasmine-marbles` |
| Guards | `TestBed.runInInjectionContext(() => guard(...))` |
| Signals | direct value checks with `()` |
| Spies | `jasmine.createSpyObj()`, `spyOn(obj, 'fn')` |
| Coverage | `ng test --code-coverage`, target >85% |
