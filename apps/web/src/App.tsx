import { useEffect, useState } from 'react';
import { getCurrentUser, type SessionUser, type UserProfile } from './api';
import { AuthProvider } from './auth/AuthContext';
import { MapView } from './map/MapView';
import { ProfilePage } from './ui/ProfilePage';
import { SettingsPage } from './ui/SettingsPage';
import { VersionBanner } from './ui/VersionBanner';

/**
 * Which of the app's views this page is — decided by the URL, since each is its own page now:
 * the Go server renders `/`, `/profile` and `/settings` as the same shell around this app
 * (ADR-0012), and links between them are ordinary links. Lives inside AuthProvider, not App
 * itself, so it can assume a signed-in user unconditionally rather than re-checking one.
 */
function AuthenticatedApp() {
  const path = window.location.pathname;
  if (path === '/profile') return <ProfilePage />;
  if (path === '/settings') return <SettingsPage />;
  return <MapView initialPrivateLocationsOpen={openPrivateLocations} />;
}

/** `?private-locations` (the header's account menu, Settings' "Manage on the map") is the map
 *  arriving with that window open. Read once per page load, here at module load rather than
 *  during a render — reading also strips it (so a refresh doesn't reopen the window), and a
 *  render can run more than once (StrictMode does exactly that in dev), which would see it
 *  already gone. */
const openPrivateLocations = takePrivateLocationsParam();

function takePrivateLocationsParam(): boolean {
  const params = new URLSearchParams(window.location.search);
  if (!params.has('private-locations')) return false;
  params.delete('private-locations');
  const rest = params.toString();
  window.history.replaceState(null, '', window.location.pathname + (rest ? `?${rest}` : '') + window.location.hash);
  return true;
}

/**
 * `'checking'` until `getCurrentUser` answers — the app renders nothing rather than flashing
 * before a session is confirmed. The Go server only serves this app to a signed-in, verified
 * (or demo) account; the redirects below are the fallback for a session that ended since.
 */
type AuthState = 'checking' | SessionUser;

export function App() {
  const [auth, setAuth] = useState<AuthState>('checking');

  useEffect(() => {
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

  const updateUser = (patch: UserProfile) => setAuth({ ...auth, ...patch });
  const authValue = { user: auth, updateUser };

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
