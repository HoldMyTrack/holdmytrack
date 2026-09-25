import { useEffect, useState } from 'react';
import { getCurrentUser, type SessionUser } from './api';
import { AuthProvider } from './auth/AuthContext';
import { MapView } from './map/MapView';
import { VersionBanner } from './ui/VersionBanner';

/**
 * The map — the one page this app is (ADR-0012): the Go server renders `/` as a shell around
 * it, with the shared header above, and every other page is the server's own. Lives inside
 * AuthProvider, not App itself, so it can assume a signed-in user unconditionally rather
 * than re-checking one.
 */
function AuthenticatedApp() {
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

  const authValue = { user: auth };

  return (
    <>
      <VersionBanner />
      <AuthProvider value={authValue}>
        <AuthenticatedApp />
      </AuthProvider>
    </>
  );
}
