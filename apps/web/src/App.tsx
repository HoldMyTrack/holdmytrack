import { useEffect, useState } from 'react';
import { t } from './i18n';
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
  return (
    <MapView initialPrivateLocationsOpen={openPrivateLocations} initialActivity={openActivity} />
  );
}

/** `?private-locations` is the map arriving with the Activities panel on its Privacy tab. Read once per page load, here at module load rather than
 *  during a render — reading also strips it (so a refresh doesn't reopen the window), and a
 *  render can run more than once (StrictMode does exactly that in dev), which would see it
 *  already gone. */
const openPrivateLocations = takePrivateLocationsParam();

/** `?activity=<id>&day=<YYYY-MM-DD>` is the map arriving on one activity — the /sync page's
 *  "View on map" (`SPEC.md` FR-3.9), which works out the activity's day on the server. Taken
 *  once and stripped, like `?private-locations`, so a refresh doesn't narrow the range again. */
const openActivity = takeActivityParam();

function takeActivityParam(): { id: string; day: string } | null {
  const params = new URLSearchParams(window.location.search);
  const id = params.get('activity');
  const day = params.get('day');
  if (id === null) return null;
  params.delete('activity');
  params.delete('day');
  const rest = params.toString();
  window.history.replaceState(null, '', window.location.pathname + (rest ? `?${rest}` : '') + window.location.hash);
  return day !== null && /^\d{4}-\d{2}-\d{2}$/.test(day) ? { id, day } : null;
}

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
 * `failed` is the answer not arriving at all — a network error, a 5xx — which says nothing
 * about the session: sending that to /signin would bounce straight back here, since the
 * server's sign-in page sends a signed-in visitor to `/`.
 */
type AuthState = 'checking' | { failed: string } | SessionUser;

export function App() {
  const [auth, setAuth] = useState<AuthState>('checking');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    getCurrentUser()
      .then((user) => {
        // No session → the sign-in page; a real account that hasn't confirmed its email →
        // the page that says so (the map has nothing to show it yet).
        if (!user) window.location.replace('/signin');
        else if ('email' in user && !user.emailVerified) window.location.replace('/verify-pending');
        else setAuth(user);
      })
      .catch((err: unknown) =>
        // fetch rejects with a TypeError ("Failed to fetch") when the server can't be reached at all.
        setAuth({ failed: err instanceof TypeError ? t('common.network_error') : err instanceof Error ? err.message : String(err) }),
      );
  }, [attempt]);

  if (auth === 'checking') return <VersionBanner />;
  if ('failed' in auth) {
    return (
      <div className="app-load-error" role="alert">
        <p className="confirm-dialog__message">{t('app.load_failed', { message: auth.failed })}</p>
        <button
          type="button"
          className="settings-page__button"
          onClick={() => {
            setAuth('checking');
            setAttempt((n) => n + 1);
          }}
        >
          {t('app.try_again')}
        </button>
      </div>
    );
  }

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
