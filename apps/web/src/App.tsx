import { useEffect, useState } from 'react';
import { getCurrentUser, logout, type SessionUser, type UserProfile } from './api';
import { AuthProvider } from './auth/AuthContext';
import { MapView } from './map/MapView';
import { clearSavedView } from './map/viewState';
import { ProfilePage } from './ui/ProfilePage';
import { SettingsPage } from './ui/SettingsPage';
import { VersionBanner } from './ui/VersionBanner';

/**
 * Three screens, switched by plain local state rather than a router: there is no URL to
 * bookmark or share for Profile or Settings (both are private to whichever account is
 * signed in — see ActivityGraph.tsx), and adding a routing library for a couple of extra
 * views would be more machinery than the app has views to justify. Lives inside
 * AuthProvider, not App itself, so it can assume a signed-in user unconditionally rather
 * than re-checking one.
 */
function AuthenticatedApp() {
  const [view, setView] = useState<'map' | 'profile' | 'settings' | 'private-locations'>('map');
  if (view === 'profile') return <ProfilePage onBack={() => setView('map')} onOpenSettings={() => setView('settings')} />;
  if (view === 'settings') {
    return (
      <SettingsPage
        onBack={() => setView('map')}
        onOpenProfile={() => setView('profile')}
        onOpenPrivateLocations={() => setView('private-locations')}
      />
    );
  }
  // 'private-locations' is the map, arriving with that window already open (Settings' link).
  return (
    <MapView
      onOpenProfile={() => setView('profile')}
      onOpenSettings={() => setView('settings')}
      initialPrivateLocationsOpen={view === 'private-locations'}
    />
  );
}

/**
 * `'checking'` until `getCurrentUser` answers — the map renders nothing rather than flashing
 * before a session is confirmed. Signed out, or signed in but unverified, never reaches a
 * state here: those leave for the server-rendered pages instead (see `landing` below).
 */
type AuthState = 'checking' | SessionUser;

/**
 * Where a visit to `/` has to go instead of the map, if anywhere. The sign-in, sign-up,
 * password-reset and email-verification screens are server-rendered pages now (ADR-0012,
 * IMPLEMENTATION.md §4.19), not this app's. Emails sent before that linked here with a query
 * param (`?reset_token=`, `?verify_token=`), and Google's callback used to report a failure
 * as `?auth_error=`; those still arrive for a while, so they're forwarded to the page that
 * handles each now. Goes away once `/` itself is served by the Go server (ADR-0012's next step).
 */
function legacyLanding(): string | null {
  const params = new URLSearchParams(window.location.search);
  const reset = params.get('reset_token');
  if (reset) return `/reset?token=${encodeURIComponent(reset)}`;
  const verify = params.get('verify_token');
  if (verify) return `/verify?token=${encodeURIComponent(verify)}`;
  if (params.get('auth_error')) return '/signin?error=google';
  return null;
}

export function App() {
  const [auth, setAuth] = useState<AuthState>('checking');

  useEffect(() => {
    const legacy = legacyLanding();
    if (legacy) {
      window.location.replace(legacy);
      return;
    }
    getCurrentUser()
      .then((user) => {
        // No session → the sign-in page; a real account that hasn't confirmed its email →
        // the page that says so (the map has nothing to show it yet).
        if (!user) window.location.replace('/signin');
        else if ('email' in user && !user.emailVerified) window.location.replace('/verify-pending');
        else setAuth(user);
      })
      .catch(() => window.location.replace('/signin'));
  }, []);

  if (auth === 'checking') return <VersionBanner />;

  // Clears the camera position the URL hash carries before leaving, so whoever signs in next
  // on this browser doesn't inherit it (clearSavedView's own doc comment, viewState.ts).
  const signOut = async () => {
    await logout();
    clearSavedView();
    window.location.assign('/signin');
  };
  const updateUser = (patch: UserProfile) => setAuth({ ...auth, ...patch });
  // UserMenu's "Create your own account" (a demo session only) — the sign-up page, which
  // offers "Back to the map" in place of another demo while one is held.
  const requestUpgrade = () => window.location.assign('/signup');
  const authValue = { user: auth, signOut, requestUpgrade, updateUser };

  // First run (FR-1.7): a verified real account with no Country confirms Country and Timezone
  // before seeing anything — both decide how every number and day in the app reads. Derived
  // from the data rather than a separate "onboarded" flag: Country can't be saved empty any
  // more, so an empty one means Settings has never been saved. Saving goes through
  // updateUser, which sets `country` and lets this fall through to the map on the next render.
  // A demo account is never gated: it's read-only and can't save Settings at all.
  if ('email' in auth && !auth.country) {
    return (
      <>
        <VersionBanner />
        <AuthProvider value={authValue}>
          <SettingsPage onboarding />
        </AuthProvider>
      </>
    );
  }

  return (
    <>
      <VersionBanner />
      <AuthProvider value={authValue}>
        <AuthenticatedApp />
      </AuthProvider>
    </>
  );
}
