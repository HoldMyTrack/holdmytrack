import { useState } from 'react';
import { forgotPassword, login, resetPassword, signup, startDemo, type SessionUser } from '../api';

export interface AuthGateProps {
  /** Called once a session actually exists — App.tsx swaps this screen out for the real app. */
  onAuthenticated: (user: SessionUser) => void;
  /** Present only when reached from an already-live session (UserMenu's "Demo session — save
   *  this", IMPLEMENTATION.md §4.10) rather than before any session exists at
   *  all. Swaps "Try it now — no signup" for a "← Back" link — starting a second demo while
   *  already in one would just abandon the first, silently — and starts the form in signup
   *  mode instead of signin, since "save this" only ever means creating an account. */
  onCancel?: () => void;
  /** Present only when the URL carried a password-reset token (App.tsx reads a `reset_token`
   *  query param once, ahead of its own session check — IMPLEMENTATION.md
   *  §4.11). Overrides everything else here with a single "set a new password" screen:
   *  arriving via a clicked email link is unambiguous intent, regardless of whatever `mode`
   *  or session state would otherwise apply. */
  resetToken?: string;
}

/** The four screens this component can show — one at a time, never combined. `'form'` is the
 *  original signin/signup toggle; `'forgot'`/`'forgot-sent'` are password-recovery's own
 *  request step and its confirmation; a `resetToken` prop bypasses this union entirely (see
 *  AuthGateProps' own doc comment) since it isn't reached by clicking anything in this
 *  component at all. */
type Screen = 'form' | 'forgot' | 'forgot-sent';

/** The brand mark repeated at the top of every screen this component can show — pulled out
 *  once rather than left duplicated across four near-identical `<form>`/`<div>` returns. */
function Brand() {
  return (
    <div className="auth-gate__brand">
      <span className="app-header__mark" aria-hidden="true">
        <span className="app-header__mark-dot" />
      </span>
      FITMAP
    </div>
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
export function AuthGate({ onAuthenticated, onCancel, resetToken }: AuthGateProps) {
  const [mode, setMode] = useState<'signin' | 'signup'>(onCancel ? 'signup' : 'signin');
  const [screen, setScreen] = useState<Screen>('form');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

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
    </div>
  );
}
