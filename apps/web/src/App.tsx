import { useEffect, useState } from 'react';
import { getCurrentUser, logout, type SessionUser, type UserProfile } from './api';
import { AuthGate } from './auth/AuthGate';
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

/** Reads a one-shot query param (an emailed link's token, a sign-in redirect's error) and
 *  strips it from the URL in the same pass — a refresh must not re-trigger it, and it
 *  shouldn't linger in browser history once read. */
function takeQueryParam(name: string): string | null {
  const params = new URLSearchParams(window.location.search);
  const value = params.get(name);
  if (value) {
    params.delete(name);
    const rest = params.toString();
    const path = window.location.pathname + (rest ? `?${rest}` : '') + window.location.hash;
    window.history.replaceState(null, '', path);
  }
  return value;
}

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
  const [resetToken, setResetToken] = useState(() => takeQueryParam('reset_token'));

  // Same read-once-and-strip pattern as resetToken above, for docs/ROADMAP.md's email
  // verification link (`?verify_token=...`).
  const [verifyToken, setVerifyToken] = useState(() => takeQueryParam('verify_token'));

  // And again for a failed Google sign-in (`?auth_error=google`, google_auth.go's
  // handleGoogleCallback) — shown once on the sign-in screen, never re-shown on refresh.
  const [authError] = useState(() => takeQueryParam('auth_error'));

  useEffect(() => {
    getCurrentUser()
      .then((user) => setAuth(user ?? 'signed-out'))
      .catch(() => setAuth('signed-out'));
  }, []);

  // Every app-initiated identity change (a sign-in/sign-up/demo-start completing, or a
  // sign-out) clears whatever camera position the URL hash carries before flipping `auth` —
  // otherwise MapView's next mount can inherit a previous session's leftover position and skip
  // its own fly-to-most-recent fallback entirely (see clearSavedView's own doc comment,
  // viewState.ts). Deliberately *not* called on the mount-time getCurrentUser check above,
  // where an existing hash is a legitimate same-session "return to where I was" on a plain
  // page refresh, not a stale leftover.
  const handleAuthenticated = (user: SessionUser) => {
    clearSavedView();
    setAuth(user);
  };
  const handleSignOut = async () => {
    await logout();
    clearSavedView();
    setAuth('signed-out');
  };

  if (resetToken) {
    return (
      <>
        <VersionBanner />
        <AuthGate
          resetToken={resetToken}
          onAuthenticated={(user) => {
            handleAuthenticated(user);
            setResetToken(null);
          }}
        />
      </>
    );
  }

  if (verifyToken) {
    return (
      <>
        <VersionBanner />
        <AuthGate
          verifyToken={verifyToken}
          onAuthenticated={(user) => {
            handleAuthenticated(user);
            setVerifyToken(null);
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
        <AuthGate onAuthenticated={handleAuthenticated} authError={authError} />
      </>
    );
  }

  // A real (non-demo) account whose email isn't confirmed yet — docs/ROADMAP.md's "Email
  // verification + demo without real ingest" gate. Checked ahead of `upgrading` and the real
  // app below: an unverified account has nothing to show yet regardless of anything else.
  if ('email' in auth && !auth.emailVerified) {
    return (
      <>
        <VersionBanner />
        <AuthGate unverifiedUser={auth} onAuthenticated={handleAuthenticated} onSignOut={() => void handleSignOut()} />
      </>
    );
  }

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
            handleAuthenticated(user);
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
  const authValue = { user: auth, signOut: handleSignOut, requestUpgrade: () => setUpgrading(true), updateUser };

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
