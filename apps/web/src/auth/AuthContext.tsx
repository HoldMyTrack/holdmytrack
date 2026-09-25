import { createContext, useContext } from 'react';
import type { SessionUser, UserProfile } from '../api';

/**
 * The signed-in user (real or demo), reachable from anywhere without prop-drilling — the
 * unit system (units.ts), MapView's demo check, SettingsPage's form. Signing out and the
 * account menu are the server-rendered header's (ADR-0012), not this app's. Only ever provided once `App.tsx` has confirmed a session
 * exists — nothing renders `<AuthProvider>` around a `null` user, so `useAuth()` inside the
 * authenticated app never has to handle "no user" as a case of its own. Narrow a `DemoUser`
 * from an `AuthUser` with `'email' in user` — see api.ts's `SessionUser` for why that's
 * enough without a separate discriminant tag.
 */
export interface AuthContextValue {
  user: SessionUser;
  /** Merges a Settings-page save's response into the live `user` — SettingsPage.tsx calls
   *  this right after `updateSettings`/`uploadAvatar`/`removeAvatar` resolve, so the unit
   *  system (`units.ts`) and the onboarding gate (App.tsx) pick up the change immediately,
   *  with no second `getCurrentUser()` round trip. (The header's avatar is server-rendered, so
   *  it shows a new one on the next page load.) */
  updateUser: (patch: UserProfile) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export const AuthProvider = AuthContext.Provider;

/** Throws outside an <AuthProvider> rather than returning null — every caller only ever
 *  renders inside one, so a null return would just move the
 *  "did I forget to check" bug from here to every call site instead of catching it once. */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth() called outside an AuthProvider');
  return ctx;
}
