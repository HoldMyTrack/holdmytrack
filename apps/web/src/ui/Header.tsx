import type { ReactNode } from 'react';
import { InfoMenu } from './InfoMenu';
import { DonateButton } from './DonateButton';
import { UserMenu } from './UserMenu';
import logoUrl from '../assets/logo.png';

/**
 * Docked top chrome: the brand mark and tagline, then Donate, the export control, the Info
 * menu and the account menu at the trailing end. Info is a text menu rather than a
 * bordered button like the actions before it: it's navigation, not something to do with your
 * data.
 * The Activities toggle that used to live here is gone — the panel it opened is now a
 * permanent left sidebar (ActivitiesPanel, rendered directly by MapView), not
 * something to show or hide, since there is always at least one activity to look at once
 * anything has been imported.
 *
 * Export is passed in as an element rather than constructed here: its wiring needs the map
 * instance, which lives in MapView, not here. Import used to sit beside it the same way; it is
 * now the Activities panel's Sync tab (SyncTab.tsx). Donate and the account menu are
 * self-contained widgets with no such wiring (see their own files), so they are rendered
 * directly. Donate leads rather than trailing off the end, since donations are how HoldMyTrack
 * is funded rather than a footnote (docs/VISION.md §6).
 */
export interface HeaderProps {
  /** Rendered between Donate and the Info menu. */
  exportControl?: ReactNode;
  /** Makes the brand mark a "go back to the map" control — only ProfilePage and SettingsPage
   *  pass this; the map screen itself has nowhere more "home" to go, so its own Header omits
   *  it and the brand mark there stays inert. */
  onBrandClick?: () => void;
  /** The account menu's "Profile" item navigates to it — passed by the map screen and by
   *  SettingsPage (so either secondary screen can jump straight to the other, not just back
   *  to the map first). ProfilePage itself omits this, since it has nowhere further to open
   *  "Profile" to when it's already open — reported live as the two secondary screens' menus
   *  otherwise looking inconsistent with each other, each missing a link the other one had. */
  onOpenProfile?: () => void;
  /** Same shape as onOpenProfile, for the account menu's "Settings" item — passed by the map
   *  screen and by ProfilePage; SettingsPage omits it for the matching reason. */
  onOpenSettings?: () => void;
  /** The account menu's "Private locations" item — passed by the map screen only. */
  onOpenPrivateLocations?: () => void;
}

export function Header({ exportControl, onBrandClick, onOpenProfile, onOpenSettings, onOpenPrivateLocations }: HeaderProps) {
  const brand = (
    <>
      <img className="app-header__logo" src={logoUrl} alt="" aria-hidden="true" />
      <span className="app-header__wordmark">
        <span className="app-header__wordmark-light">HoldMy</span>
        <span className="app-header__wordmark-bold">Track</span>
      </span>
    </>
  );
  return (
    <header className="app-header">
      {onBrandClick ? (
        <button type="button" className="app-header__brand app-header__brand--button" onClick={onBrandClick}>
          {brand}
        </button>
      ) : (
        <div className="app-header__brand">{brand}</div>
      )}
      <span className="app-header__divider" aria-hidden="true" />
      <span className="app-header__tagline">Every journey, mapped.</span>
      <nav className="app-header__actions" aria-label="Main">
        <DonateButton />
        {exportControl}
        <InfoMenu />
        <UserMenu onOpenProfile={onOpenProfile} onOpenSettings={onOpenSettings} onOpenPrivateLocations={onOpenPrivateLocations} />
      </nav>
    </header>
  );
}
