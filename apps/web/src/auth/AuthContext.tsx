import { createContext, useContext } from 'react';
import type { SessionUser } from '../api';

/**
 * The signed-in user (real or demo), reachable from anywhere without prop-drilling — the
 * unit system (units.ts), MapView's demo check. Signing out, the account menu and Settings
 * are server-rendered pages (ADR-0012), not this app's; a Settings change reaches this app on
 * its next page load, which is also the only way back to it from Settings. Only ever provided once `App.tsx` has confirmed a session
 * exists — nothing renders `<AuthProvider>` around a `null` user, so `useAuth()` inside the
 * authenticated app never has to handle "no user" as a case of its own. Narrow a `DemoUser`
 * from an `AuthUser` with `'email' in user` — see api.ts's `SessionUser` for why that's
 * enough without a separate discriminant tag.
 */
export interface AuthContextValue {
  user: SessionUser;
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
