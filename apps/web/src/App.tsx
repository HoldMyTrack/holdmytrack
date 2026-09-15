import { useEffect, useState } from 'react';
import { getCurrentUser, logout, type SessionUser, type UserProfile } from './api';
import { AuthGate } from './auth/AuthGate';
import { AuthProvider } from './auth/AuthContext';
import { MapView } from './map/MapView';
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
  const [view, setView] = useState<'map' | 'profile' | 'settings'>('map');
  if (view === 'profile') return <ProfilePage onBack={() => setView('map')} onOpenSettings={() => setView('settings')} />;
  if (view === 'settings') return <SettingsPage onBack={() => setView('map')} onOpenProfile={() => setView('profile')} />;
  return <MapView onOpenProfile={() => setView('profile')} onOpenSettings={() => setView('settings')} />;
}

/**
 * `null` (checking), a real or demo user, or the string `'signed-out'` — not just
 * `SessionUser | null`, so "haven't asked yet" and "asked, and there's no session" render
 * differently: the former shows nothing rather than flashing the sign-in form for the common
 * case (a valid session cookie already present) before `getCurrentUser` resolves.
 */
type AuthState = 'checking' | 'signed-out' | SessionUser;

export function App() {
  const [auth, setAuth] = useState<AuthState>('checking');
  // UserMenu's "Demo session — save this" (IMPLEMENTATION.md §4.10) — turning
  // a demo into a real account means showing AuthGate again, the exact same page a new
  // visitor sees, not a second bespoke form. Lives here rather than as a fourth AuthState
  // value because it's orthogonal to `auth` itself: the session doesn't change (or even get
  // touched) until AuthGate's own onAuthenticated fires below.
  const [upgrading, setUpgrading] = useState(false);
  // A password-reset email's link (IMPLEMENTATION.md §4.11) lands here as
  // `?reset_token=...` — read once, lazily, and stripped from the URL in the same pass (a
  // refresh mid-form must not re-trigger it, and it shouldn't linger in browser history once
  // read). Checked independently of `auth`, ahead of every branch below: arriving via a
  // clicked email link is unambiguous intent, outranking even an already-live session.
  const [resetToken, setResetToken] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    const token = params.get('reset_token');
    if (token) {
      params.delete('reset_token');
      const rest = params.toString();
      const path = window.location.pathname + (rest ? `?${rest}` : '') + window.location.hash;
      window.history.replaceState(null, '', path);
    }
    return token;
  });

  useEffect(() => {
    getCurrentUser()
      .then((user) => setAuth(user ?? 'signed-out'))
      .catch(() => setAuth('signed-out'));
  }, []);

  if (resetToken) {
    return (
      <>
        <VersionBanner />
        <AuthGate
          resetToken={resetToken}
          onAuthenticated={(user) => {
            setAuth(user);
            setResetToken(null);
          }}
        />
      </>
    );
  }

  if (auth === 'checking') return <VersionBanner />;
  if (auth === 'signed-out') {
    return (
      <>
        <VersionBanner />
        <AuthGate onAuthenticated={setAuth} />
      </>
    );
  }

  const signOut = async () => {
    await logout();
    setAuth('signed-out');
  };

  // AuthenticatedApp (and MapView with it) fully unmounts for this — a deliberate tradeoff,
  // not an oversight: the uploaded-during-the-demo data is server-side and untouched either
  // way, but the map's own transient view state (camera, filters, selection) resets on
  // cancel. Accepted in exchange for reusing AuthGate exactly as it already is, rather than
  // building a second overlay-only presentation of the same form just to keep that state.
  if (upgrading) {
    return (
      <>
        <VersionBanner />
        <AuthGate
          onAuthenticated={(user) => {
            setAuth(user);
            setUpgrading(false);
          }}
          onCancel={() => setUpgrading(false)}
        />
      </>
    );
  }

  // `auth` is already narrowed to a real SessionUser by the two early returns above, so this
  // closure can merge directly into it rather than needing setAuth's functional-updater form.
  const updateUser = (patch: UserProfile) => setAuth({ ...auth, ...patch });

  return (
    <>
      <VersionBanner />
      <AuthProvider value={{ user: auth, signOut, requestUpgrade: () => setUpgrading(true), updateUser }}>
        <AuthenticatedApp />
      </AuthProvider>
    </>
  );
}
