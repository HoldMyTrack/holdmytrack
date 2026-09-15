import { useEffect, useRef, useState } from 'react';
import { API_BASE_URL } from '../api';
import { useAuth } from '../auth/AuthContext';

/**
 * The header's account menu — an avatar button and a dropdown. Real accounts exist now
 * (services/server/internal/httpapi/auth.go) — "Sign out" ends the actual session, and the
 * dropdown shows the signed-in user's real email, which would have been dishonest to invent
 * back when nobody was actually signed in (the placeholder-user era this comment used to
 * describe). "Profile" (§4.8's activity graph, ProfilePage.tsx) predates real accounts —
 * built against the single seeded user first — and needed no changes here once they landed;
 * it already just shows whichever account is current. "Settings" (SettingsPage.tsx) is where
 * that avatar actually gets set — the button here shows it once one exists, falling back to
 * the generic glyph until then rather than initials, matching how a not-yet-set field reads
 * elsewhere in this app (an em dash, not a fabricated stand-in).
 *
 * A demo session (VISION.md §8.2, AuthGate's "Try it now") shows "Demo session — save
 * this" here in place of an email — `requestUpgrade()` hands off to App.tsx, which swaps the
 * whole screen to the exact same AuthGate a new visitor sees (IMPLEMENTATION.md
 * §4.10) rather than a second, bespoke form living in this dropdown.
 */
export interface UserMenuProps {
  /** Navigates to ProfilePage. Optional (and explicitly `| undefined`, not just `?:` — see
   *  tsconfig's exactOptionalPropertyTypes) so Header can pass its own same-shaped optional
   *  prop straight through without a conditional spread. Omitted/undefined when rendering the
   *  menu from ProfilePage itself, which has nowhere further to open "Profile" to. */
  onOpenProfile?: (() => void) | undefined;
  /** Same shape as onOpenProfile, for SettingsPage. */
  onOpenSettings?: (() => void) | undefined;
}

export function UserMenu({ onOpenProfile, onOpenSettings }: UserMenuProps) {
  const { user, signOut, requestUpgrade } = useAuth();
  const [open, setOpen] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Dismiss on a click anywhere else, or on Escape — what a dropdown is expected to do, and
  // what it would otherwise not do, since nothing else here takes the click. `pointerdown`
  // rather than `click` so the menu is gone before whatever was clicked reacts.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  return (
    <div className="user-menu" ref={rootRef} data-testid="user-menu">
      <button
        type="button"
        className="user-menu__avatar"
        data-testid="user-menu-button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Account menu"
        onClick={() => setOpen((was) => !was)}
      >
        {user.avatarUrl ? (
          <img src={`${API_BASE_URL}${user.avatarUrl}`} crossOrigin="use-credentials" alt="" />
        ) : (
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <circle cx="12" cy="8.5" r="3.75" />
            <path d="M4.5 20.5a7.5 7.5 0 0 1 15 0" />
          </svg>
        )}
      </button>

      {open && (
        <div className="user-menu__dropdown" role="menu" data-testid="user-menu-dropdown">
          {'email' in user ? (
            <div className="user-menu__email" data-testid="user-menu-email">
              {user.email}
            </div>
          ) : (
            <button
              type="button"
              role="menuitem"
              className="user-menu__item user-menu__item--accent"
              data-testid="user-menu-save"
              onClick={() => {
                setOpen(false);
                requestUpgrade();
              }}
            >
              Demo session — save this
            </button>
          )}
          {onOpenProfile && (
            <button
              type="button"
              role="menuitem"
              className="user-menu__item"
              data-testid="user-menu-profile"
              onClick={() => {
                setOpen(false);
                onOpenProfile();
              }}
            >
              Profile
            </button>
          )}
          {onOpenSettings && (
            <button
              type="button"
              role="menuitem"
              className="user-menu__item"
              data-testid="user-menu-settings"
              onClick={() => {
                setOpen(false);
                onOpenSettings();
              }}
            >
              Settings
            </button>
          )}
          <button
            type="button"
            role="menuitem"
            className="user-menu__item"
            disabled={signingOut}
            onClick={() => {
              setSigningOut(true);
              // Not setOpen(false) first — App.tsx unmounts this whole menu the moment
              // signOut() resolves (back to AuthGate), so there is no dropdown left open to
              // close by then anyway.
              void signOut().finally(() => setSigningOut(false));
            }}
          >
            {signingOut ? 'Signing out…' : 'Sign out'}
          </button>
        </div>
      )}
    </div>
  );
}
