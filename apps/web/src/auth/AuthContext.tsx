import { createContext, useContext } from 'react';
import type { SessionUser, UserProfile } from '../api';

/**
 * The signed-in user (real or demo), reachable from anywhere without prop-drilling through
 * MapView → Header → UserMenu (three levels, for a single leaf that needs it) or
 * ProfilePage's own separate tree. Only ever provided once `App.tsx` has confirmed a session
 * exists — nothing renders `<AuthProvider>` around a `null` user, so `useAuth()` inside the
 * authenticated app never has to handle "no user" as a case of its own. Narrow a `DemoUser`
 * from an `AuthUser` with `'email' in user` — see api.ts's `SessionUser` for why that's
 * enough without a separate discriminant tag.
 */
export interface AuthContextValue {
  user: SessionUser;
  /** Ends the session server-side and clears it here — App.tsx re-renders back to AuthGate
   *  once this resolves, since `user` becoming unreachable is what that gate is keyed on. */
  signOut: () => Promise<void>;
  /** UserMenu's "Demo session — save this" — App.tsx is the only thing that can act on this,
   *  since turning a demo into a real account means swapping the whole screen to AuthGate
   *  (IMPLEMENTATION.md §4.10), which only the top-level view switch owns. */
  requestUpgrade: () => void;
  /** Merges a Settings-page save's response into the live `user` — SettingsPage.tsx calls
   *  this right after `updateSettings`/`uploadAvatar`/`removeAvatar` resolve, so the header
   *  avatar and the unit system (`units.ts`) pick up the change everywhere immediately,
   *  with no reload and no second `getCurrentUser()` round trip. */
  updateUser: (patch: UserProfile) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export const AuthProvider = AuthContext.Provider;

/** Throws outside an <AuthProvider> rather than returning null — every caller (UserMenu, any
 *  future per-user UI) only ever renders inside one, so a null return would just move the
 *  "did I forget to check" bug from here to every call site instead of catching it once. */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth() called outside an AuthProvider');
  return ctx;
}
