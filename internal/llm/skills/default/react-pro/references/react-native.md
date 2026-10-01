# React Native + Expo Router

React Native 0.74+ / Expo SDK 51+ / Expo Router v3 / Reanimated 3. Load when building mobile screens, tuning list performance, handling platform-specific code, or wiring persistent storage.

## Project structure (Expo Router)

```
app/
├── _layout.tsx           # Root layout
├── index.tsx             # Home (/)
├── +not-found.tsx        # 404
├── (tabs)/               # Tab group
│   ├── _layout.tsx       # Tab bar config
│   ├── index.tsx         # First tab
│   └── profile.tsx       # Profile tab
├── (auth)/               # Auth group
│   ├── _layout.tsx
│   └── login.tsx
└── details/[id].tsx      # Dynamic route
```

## Root & tab layouts

```tsx
// app/_layout.tsx
import { Stack } from 'expo-router';
import { ThemeProvider, DefaultTheme, DarkTheme } from '@react-navigation/native';
import { useColorScheme } from 'react-native';

export default function RootLayout() {
  const scheme = useColorScheme();
  return (
    <ThemeProvider value={scheme === 'dark' ? DarkTheme : DefaultTheme}>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="(auth)" />
        <Stack.Screen name="details/[id]" options={{ presentation: 'modal' }} />
      </Stack>
    </ThemeProvider>
  );
}
```

```tsx
// app/(tabs)/_layout.tsx
import { Tabs } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';

export default function TabLayout() {
  return (
    <Tabs screenOptions={{ tabBarActiveTintColor: '#0a7ea4', headerShown: true }}>
      <Tabs.Screen
        name="index"
        options={{
          title: 'Home',
          tabBarIcon: ({ color, size }) => <Ionicons name="home" color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        name="profile"
        options={{
          title: 'Profile',
          tabBarIcon: ({ color, size }) => <Ionicons name="person" color={color} size={size} />,
        }}
      />
    </Tabs>
  );
}
```

## Navigation

```tsx
import { router, useLocalSearchParams, Link, Redirect } from 'expo-router';

router.push('/details/123');
router.replace('/home');
router.back();

router.push({
  pathname: '/details/[id]',
  params: { id: '123', title: 'Item' },
});

<Link href="/profile" asChild>
  <Pressable><Text>Go to Profile</Text></Pressable>
</Link>;

function DetailsScreen() {
  const { id, title } = useLocalSearchParams<{ id: string; title?: string }>();
  return <Text>Details for {id}</Text>;
}
```

### Protected routes

```tsx
// app/(auth)/_layout.tsx
import { Redirect, Stack } from 'expo-router';
import { useAuth } from '@/hooks/use-auth';

export default function AuthLayout() {
  const { user, isLoading } = useAuth();
  if (isLoading) return <LoadingScreen />;
  if (user) return <Redirect href="/(tabs)" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
```

## Optimized FlatList (memo + useCallback + getItemLayout)

```tsx
import React, { memo, useCallback, useRef } from 'react';
import { FlatList, Pressable, Text, View, type ListRenderItem } from 'react-native';

interface Item { id: string; title: string; subtitle: string }
const ITEM_HEIGHT = 72;

const ListItem = memo(function ListItem({
  item, onPress,
}: { item: Item; onPress: (id: string) => void }) {
  return (
    <Pressable onPress={() => onPress(item.id)} style={styles.item}>
      <Text style={styles.title}>{item.title}</Text>
      <Text style={styles.subtitle}>{item.subtitle}</Text>
    </Pressable>
  );
});

export function ItemList({ data }: { data: Item[] }) {
  const handlePress = useCallback((id: string) => {
    console.log('Selected:', id);
  }, []);

  const renderItem: ListRenderItem<Item> = useCallback(
    ({ item }) => <ListItem item={item} onPress={handlePress} />,
    [handlePress],
  );

  const keyExtractor = useCallback((item: Item) => item.id, []);

  const getItemLayout = useCallback(
    (_: unknown, index: number) => ({
      length: ITEM_HEIGHT,
      offset: ITEM_HEIGHT * index,
      index,
    }),
    [],
  );

  return (
    <FlatList
      data={data}
      renderItem={renderItem}
      keyExtractor={keyExtractor}
      getItemLayout={getItemLayout}
      removeClippedSubviews
      maxToRenderPerBatch={10}
      initialNumToRender={10}
      windowSize={5}
    />
  );
}
```

For very large lists, prefer `@shopify/flash-list`:

```tsx
import { FlashList } from '@shopify/flash-list';

<FlashList data={data} renderItem={renderItem} estimatedItemSize={72} keyExtractor={keyExtractor} />
```

## Forms with KeyboardAvoidingView + SafeArea

```tsx
import { KeyboardAvoidingView, Platform, ScrollView, TextInput, SafeAreaView } from 'react-native';

export function LoginForm() {
  return (
    <SafeAreaView style={{ flex: 1 }}>
      <KeyboardAvoidingView
        style={{ flex: 1 }}
        behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
        keyboardVerticalOffset={Platform.select({ ios: 88, android: 0 })}
      >
        <ScrollView contentContainerStyle={{ padding: 16, gap: 12 }} keyboardShouldPersistTaps="handled">
          <TextInput style={styles.input} placeholder="Email" autoCapitalize="none" keyboardType="email-address" />
          <TextInput style={styles.input} placeholder="Password" secureTextEntry />
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
```

## Platform handling

```tsx
import { Platform, StyleSheet } from 'react-native';

const styles = StyleSheet.create({
  card: {
    padding: 16,
    borderRadius: 12,
    backgroundColor: '#fff',
    ...Platform.select({
      ios: { shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.1, shadowRadius: 8 },
      android: { elevation: 4 },
    }),
  },
});
```

Platform-specific files: `Button.ios.tsx` / `Button.android.tsx`. Importing `'./Button'` resolves to the right one.

## SafeArea with `react-native-safe-area-context`

```tsx
import { SafeAreaProvider, useSafeAreaInsets } from 'react-native-safe-area-context';

function CustomHeader() {
  const insets = useSafeAreaInsets();
  return <View style={{ paddingTop: insets.top, paddingHorizontal: 16 }}><Text>Header</Text></View>;
}

export function App() {
  return (
    <SafeAreaProvider>
      <Navigation />
    </SafeAreaProvider>
  );
}
```

## Android back button

```tsx
import { useEffect } from 'react';
import { BackHandler, Platform } from 'react-native';

function useBackHandler(handler: () => boolean) {
  useEffect(() => {
    if (Platform.OS !== 'android') return;
    const sub = BackHandler.addEventListener('hardwareBackPress', handler);
    return () => sub.remove();
  }, [handler]);
}

// Return true to swallow the back press, false to allow default
useBackHandler(() => {
  if (hasUnsavedChanges) { showDiscardAlert(); return true; }
  return false;
});
```

## Storage: MMKV (fast) vs AsyncStorage (simple)

```tsx
import { useMMKVString, useMMKVBoolean } from 'react-native-mmkv';
import { MMKV } from 'react-native-mmkv';

const storage = new MMKV();
storage.set('user.name', 'John');
const name = storage.getString('user.name');

function Settings() {
  const [theme, setTheme] = useMMKVString('theme');
  const [notifications, setNotifications] = useMMKVBoolean('notifications');
  return (/* ... */);
}
```

### Zustand + MMKV persistence

```tsx
import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { MMKV } from 'react-native-mmkv';

const mmkv = new MMKV();
const mmkvStorage = {
  getItem: (name: string) => mmkv.getString(name) ?? null,
  setItem: (name: string, value: string) => mmkv.set(name, value),
  removeItem: (name: string) => mmkv.delete(name),
};

interface SettingsStore {
  theme: 'light' | 'dark';
  setTheme: (t: 'light' | 'dark') => void;
}

export const useSettings = create<SettingsStore>()(
  persist(
    (set) => ({ theme: 'light', setTheme: (theme) => set({ theme }) }),
    { name: 'settings', storage: createJSONStorage(() => mmkvStorage) },
  ),
);
```

## Verification gates (Native)

1. `npx expo doctor` — SDK + dependency compatibility. Fix every issue before proceeding.
2. `pnpm tsc --noEmit` — zero type errors.
3. `pnpm eslint .` — zero lint errors.
4. `npx expo start --clear` — Metro bundler boots clean. On cache errors, `npx expo start --clear`.
5. iOS: `npx expo run:ios` (or simulator). Android: `npx expo run:android` (or emulator).
6. Profile with React DevTools + Flipper for native module issues.

## Quick Reference

| Concern | Tool |
|---------|------|
| Navigation | Expo Router v3 (`Stack`, `Tabs`, `Drawer`, `Link`, `router`) |
| Lists | `FlatList` (memo + `getItemLayout`) or `@shopify/flash-list` |
| Keyboard | `KeyboardAvoidingView` + `ScrollView` |
| Notch | `react-native-safe-area-context` (`SafeAreaProvider`, `useSafeAreaInsets`) |
| Platform | `Platform.OS`, `Platform.select`, `.ios.tsx` / `.android.tsx` |
| Back button (Android) | `BackHandler.addEventListener('hardwareBackPress', fn)` |
| Storage | `react-native-mmkv` (sync, fast); `AsyncStorage` (async, simple) |
| State | Zustand + MMKV persistence; TanStack Query for server state |
| Animations | Reanimated 3 (worklets, run on UI thread) |
| Gestures | `react-native-gesture-handler` |
