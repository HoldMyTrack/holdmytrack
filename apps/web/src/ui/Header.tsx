import type { ReactNode } from 'react';
import { DonateButton } from './DonateButton';
import { UserMenu } from './UserMenu';

/**
 * Docked top chrome: the brand mark, then Donate, the import control and the account menu at
 * the trailing end. The Activities toggle that used to live here is gone — the panel it
 * opened is now a permanent left sidebar (ActivitiesPanel, rendered directly by MapView), not
 * something to show or hide, since there is always at least one activity to look at once
 * anything has been imported.
 *
 * Import and Export are both real and both passed in as elements rather than constructed
 * here, for the same reason: their wiring needs the map instance, which lives in MapView, not
 * here — keeping them props stops this component from needing to know anything about imports
 * or exports. Donate and the account menu are self-contained placeholders/widgets with no
 * such wiring to keep anywhere (see their own files), so they are rendered directly.
 *
 * The order is deliberate: Import sits nearest Export because both are the two things this
 * bar actually lets you do with your data (in, then out), Export sits nearest the account
 * menu as the newer of the two, and Donate leads into both rather than trailing off the end,
 * since donations are how FitMap is funded rather than a footnote (docs/VISION.md §5).
 */
export interface HeaderProps {
  /** Rendered between Donate and the account menu. */
  importControl?: ReactNode;
  /** Rendered between the import control and the account menu. */
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
}

export function Header({ importControl, exportControl, onBrandClick, onOpenProfile, onOpenSettings }: HeaderProps) {
  const brand = (
    <>
      <span className="app-header__mark" aria-hidden="true">
        <span className="app-header__mark-dot" />
      </span>
      FitMap
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
      <nav className="app-header__actions" aria-label="Main">
        <DonateButton />
        {importControl}
        {exportControl}
        <UserMenu onOpenProfile={onOpenProfile} onOpenSettings={onOpenSettings} />
      </nav>
    </header>
  );
}
