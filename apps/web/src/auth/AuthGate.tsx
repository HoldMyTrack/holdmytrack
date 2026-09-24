import { useEffect, useState } from 'react';
import {
  changeEmail,
  forgotPassword,
  getAuthProviders,
  googleSignInUrl,
  login,
  resendVerification,
  resetPassword,
  signup,
  startDemo,
  verifyEmail,
  type AuthUser,
  type SessionUser,
} from '../api';
import logoUrl from '../assets/logo.png';

export interface AuthGateProps {
  /** Called once a session actually exists — App.tsx swaps this screen out for the real app. */
  onAuthenticated: (user: SessionUser) => void;
  /** Present only when reached from an already-live session (UserMenu's "Create your own
   *  account", IMPLEMENTATION.md §4.10) rather than before any session exists at
   *  all. Swaps "Try it now — no signup" for a "← Back" link — starting a second demo while
   *  already in one would just abandon the first, silently — and starts the form in signup
   *  mode instead of signin, since "Create your own account" only ever means creating one. */
  onCancel?: () => void;
  /** Present only when the URL carried a password-reset token (App.tsx reads a `reset_token`
   *  query param once, ahead of its own session check — IMPLEMENTATION.md
   *  §4.11). Overrides everything else here with a single "set a new password" screen:
   *  arriving via a clicked email link is unambiguous intent, regardless of whatever `mode`
   *  or session state would otherwise apply. */
  resetToken?: string;
  /** Present only when the URL carried an email-verification token (App.tsx reads a
   *  `verify_token` query param the same way it reads `reset_token`, docs/ROADMAP.md's "Email
   *  verification + demo without real ingest"). Verifies immediately on mount — there's
   *  nothing for a person to type, unlike resetToken's form, just a link that was clicked. */
  verifyToken?: string;
  /** Present when there's already a live but unverified real session — App.tsx's own check of
   *  `emailVerified` on the signed-in user, not a URL token at all. Distinct from resetToken/
   *  verifyToken (which both mean "no session exists yet, or ignore the one that does"): this
   *  one has a real session to sign out of, hence onSignOut alongside it. */
  unverifiedUser?: AuthUser;
  onSignOut?: () => void;
  /** Present when a Google sign-in just failed (App.tsx reads `?auth_error=` once) — the
   *  server redirects back with no detail on purpose, so this only ever means "try again." */
  authError?: string | null;
}

/** The four screens this component can show — one at a time, never combined. `'form'` is the
 *  original signin/signup toggle; `'forgot'`/`'forgot-sent'` are password-recovery's own
 *  request step and its confirmation; a `resetToken`/`verifyToken`/`unverifiedUser` prop
 *  bypasses this union entirely (see AuthGateProps' own doc comments) since none of them are
 *  reached by clicking anything in this component at all. */
type Screen = 'form' | 'forgot' | 'forgot-sent';

/** The brand mark repeated at the top of every screen this component can show — pulled out
 *  once rather than left duplicated across four near-identical `<form>`/`<div>` returns. */
function Brand() {
  return (
    <div className="auth-gate__brand">
      <img className="auth-gate__logo" src={logoUrl} alt="" aria-hidden="true" />
      <span className="app-header__wordmark">
        <span className="app-header__wordmark-light">HoldMy</span>
        <span className="app-header__wordmark-bold">Track</span>
      </span>
    </div>
  );
}

/** Google's multicolor "G", as its sign-in branding guidelines require next to the label. */
function GoogleMark() {
  return (
    <svg className="auth-gate__google-mark" viewBox="0 0 48 48" aria-hidden="true">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  );
}

/**
 * The screen shown whenever there's no confirmed session to show the real app for — either
 * literally none yet (first load, or after a sign-out: `onCancel` absent), or a live demo
 * session that hasn't been claimed by a real account yet (`onCancel` present, App.tsx swaps
 * this in for `AuthenticatedApp` entirely rather than layering it over the map — see
 * App.tsx's own reasoning for why a full page swap, not a dialog, is the right cost here) —
 * or a password-reset link was just clicked (`resetToken` present, see AuthGateProps).
 * One form serves both signup and login (a toggle switches which endpoint submit calls)
 * rather than two separate routes, since this app has no router to give either one its own
 * URL anyway (App.tsx's own reasoning for switching screens with plain `useState`).
 *
 * Signing up with the same email the seeded placeholder user already holds *claims* that
 * account server-side (services/server/internal/httpapi/auth.go's claimOrCreateUser) rather
 * than erroring as a duplicate — this form has no way to know or care which case it hit;
 * both return the same shape. Signing up while a live demo session is held claims *that*
 * account instead (claimDemoUser) — same reasoning, this form still doesn't need to know
 * which case it hit.
 *
 * "Try it now — no signup" (VISION.md §8.2) calls handleDemoStart instead of either
 * endpoint above — a real session behind the scenes, so `onAuthenticated` treats it exactly
 * like a real login.
 */
export function AuthGate({ onAuthenticated, onCancel, resetToken, verifyToken, unverifiedUser, onSignOut, authError }: AuthGateProps) {
  const [mode, setMode] = useState<'signin' | 'signup'>(onCancel ? 'signup' : 'signin');
  const [screen, setScreen] = useState<Screen>('form');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(
    authError ? "Couldn't sign in with Google. Please try again." : null,
  );
  const [submitting, setSubmitting] = useState(false);

  // Hidden until the server confirms it can complete the flow (docs/SPEC.md FR-1.9) — a
  // deployment without Google credentials shows exactly the form it always did.
  const [googleEnabled, setGoogleEnabled] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void getAuthProviders().then((p) => {
      if (!cancelled) setGoogleEnabled(p.google);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // verifyToken auto-submits on mount — there's nothing for a person to type here, unlike
  // resetToken's form, just a link that was clicked. 'verifying' is the only state a person
  // ever sees unless it fails (an expired or already-used link).
  const [verifyStatus, setVerifyStatus] = useState<'verifying' | 'error'>('verifying');
  useEffect(() => {
    if (!verifyToken) return;
    verifyEmail(verifyToken)
      .then(onAuthenticated)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
        setVerifyStatus('error');
      });
    // Deliberately run once per mount, not once per verifyToken change — App.tsx only ever
    // mounts this component with a given token once (it strips the query param immediately).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // unverifiedUser's own two actions (below) — kept separate from the main form's email/
  // password state above since this screen shares none of it.
  const [newEmail, setNewEmail] = useState(unverifiedUser?.email ?? '');
  const [verifyNotice, setVerifyNotice] = useState<string | null>(null);

  const handleResend = () => {
    setError(null);
    setVerifyNotice(null);
    setSubmitting(true);
    resendVerification()
      .then(() => setVerifyNotice('Verification email sent.'))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  const handleChangeEmailSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    setVerifyNotice(null);
    setSubmitting(true);
    changeEmail(newEmail)
      .then((user) => {
        setVerifyNotice('Verification email sent to the new address.');
        onAuthenticated(user); // still unverified — App.tsx keeps showing this screen, updated.
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    const action = mode === 'signin' ? login : signup;
    action(email, password)
      .then(onAuthenticated)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  const handleDemo = () => {
    setError(null);
    setSubmitting(true);
    startDemo()
      .then(onAuthenticated)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  const handleForgotSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    forgotPassword(email)
      .then(() => setScreen('forgot-sent'))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  const handleResetSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    if (!resetToken) return;
    setError(null);
    setSubmitting(true);
    resetPassword(resetToken, password)
      .then(onAuthenticated)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSubmitting(false));
  };

  if (verifyToken) {
    return (
      <div className="auth-gate">
        <div className="auth-gate__card">
          <Brand />
          <h1 className="auth-gate__title">Verifying your email…</h1>
          {verifyStatus === 'error' && error && (
            <p className="auth-gate__error" role="alert">
              {error}
            </p>
          )}
        </div>
      </div>
    );
  }

  if (unverifiedUser) {
    return (
      <div className="auth-gate">
        <div className="auth-gate__card">
          <Brand />
          <h1 className="auth-gate__title">Verify your email</h1>
          <p className="auth-gate__hint">
            We sent a confirmation link to <strong>{unverifiedUser.email}</strong>. Click it to unlock your account.
          </p>

          {verifyNotice && <p className="auth-gate__hint">{verifyNotice}</p>}
          {error && (
            <p className="auth-gate__error" role="alert">
              {error}
            </p>
          )}

          <button type="button" className="auth-gate__submit" onClick={handleResend} disabled={submitting}>
            {submitting ? 'Please wait…' : 'Resend verification email'}
          </button>

          <form className="auth-gate__field" onSubmit={handleChangeEmailSubmit}>
            <span>Typed the wrong address? Change it:</span>
            <input
              type="email"
              required
              autoComplete="email"
              value={newEmail}
              onChange={(e) => setNewEmail(e.target.value)}
              disabled={submitting}
            />
            <button type="submit" className="auth-gate__toggle" disabled={submitting}>
              Save new email
            </button>
          </form>

          {onSignOut && (
            <button type="button" className="auth-gate__toggle" onClick={onSignOut} disabled={submitting}>
              ← Sign out
            </button>
          )}
        </div>
      </div>
    );
  }

  if (resetToken) {
    return (
      <div className="auth-gate">
        <form className="auth-gate__card" onSubmit={handleResetSubmit}>
          <Brand />
          <h1 className="auth-gate__title">Set a new password</h1>

          <label className="auth-gate__field">
            <span>New password</span>
            <input
              type="password"
              required
              minLength={8}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={submitting}
            />
          </label>

          {error && (
            <p className="auth-gate__error" role="alert">
              {error}
            </p>
          )}

          <button type="submit" className="auth-gate__submit" disabled={submitting}>
            {submitting ? 'Please wait…' : 'Set new password'}
          </button>
        </form>
      </div>
    );
  }

  if (screen === 'forgot-sent') {
    return (
      <div className="auth-gate">
        <div className="auth-gate__card">
          <Brand />
          <h1 className="auth-gate__title">Check your email</h1>
          <p className="auth-gate__hint">
            If an account exists for that email, a reset link is on its way — it works once and
            expires in an hour.
          </p>
          <button
            type="button"
            className="auth-gate__toggle"
            onClick={() => {
              setScreen('form');
              setMode('signin');
            }}
          >
            ← Back to sign in
          </button>
        </div>
      </div>
    );
  }

  if (screen === 'forgot') {
    return (
      <div className="auth-gate">
        <form className="auth-gate__card" onSubmit={handleForgotSubmit}>
          <Brand />
          <h1 className="auth-gate__title">Reset your password</h1>

          <label className="auth-gate__field">
            <span>Email</span>
            <input
              type="email"
              required
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={submitting}
            />
          </label>

          {error && (
            <p className="auth-gate__error" role="alert">
              {error}
            </p>
          )}

          <button type="submit" className="auth-gate__submit" disabled={submitting}>
            {submitting ? 'Please wait…' : 'Send reset link'}
          </button>

          <button
            type="button"
            className="auth-gate__toggle"
            onClick={() => {
              setError(null);
              setScreen('form');
            }}
            disabled={submitting}
          >
            ← Back to sign in
          </button>
        </form>
      </div>
    );
  }

  return (
    <div className="auth-gate">
      <form className="auth-gate__card" onSubmit={handleSubmit}>
        <Brand />
        <h1 className="auth-gate__title">{mode === 'signin' ? 'Sign in' : 'Create your account'}</h1>

        {googleEnabled && (
          <>
            {/* A plain link, not a button handler: the whole flow is full-page navigations
                through the server and Google, with no Google script on this page. */}
            <a className="auth-gate__google" href={googleSignInUrl()} aria-disabled={submitting}>
              <GoogleMark />
              Continue with Google
            </a>
            <div className="auth-gate__divider" role="separator">
              or
            </div>
          </>
        )}

        <label className="auth-gate__field">
          <span>Email</span>
          <input
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={submitting}
          />
        </label>
        <label className="auth-gate__field">
          <span>Password</span>
          <input
            type="password"
            required
            minLength={8}
            autoComplete={mode === 'signin' ? 'current-password' : 'new-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={submitting}
          />
        </label>

        {mode === 'signin' && (
          <button
            type="button"
            className="auth-gate__forgot"
            onClick={() => {
              setError(null);
              setScreen('forgot');
            }}
            disabled={submitting}
          >
            Forgot password?
          </button>
        )}

        {error && (
          <p className="auth-gate__error" role="alert">
            {error}
          </p>
        )}

        <button type="submit" className="auth-gate__submit" disabled={submitting}>
          {submitting ? 'Please wait…' : mode === 'signin' ? 'Sign in' : 'Create account'}
        </button>

        <button
          type="button"
          className="auth-gate__toggle"
          onClick={() => {
            setError(null);
            setMode((m) => (m === 'signin' ? 'signup' : 'signin'));
          }}
          disabled={submitting}
        >
          {mode === 'signin' ? "Don't have an account? Create one" : 'Already have an account? Sign in'}
        </button>

        {onCancel ? (
          <button type="button" className="auth-gate__toggle" onClick={onCancel} disabled={submitting}>
            ← Back
          </button>
        ) : (
          <>
            <div className="auth-gate__divider" role="separator">
              or
            </div>

            <button type="button" className="auth-gate__demo" onClick={handleDemo} disabled={submitting}>
              Try it now — no signup
            </button>
          </>
        )}
      </form>
      <a className="auth-gate__about" href="/about">
        What is HoldMyTrack?
      </a>
    </div>
  );
}
