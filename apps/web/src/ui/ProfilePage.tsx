import { ActivityGraph } from './ActivityGraph';
import { Header } from './Header';
import { Trends } from './Trends';

export interface ProfilePageProps {
  onBack: () => void;
  /** Lets the account menu jump straight to Settings without detouring back through the map
   *  first — the same cross-link SettingsPage offers back to Profile. Reported live as the
   *  two secondary screens' menus otherwise being inconsistent with each other. */
  onOpenSettings: () => void;
}

/**
 * §4.8's activity graph, reached from the account menu's "Profile" item (UserMenu.tsx). Just
 * the "ACTIVITY GRID" panel and the performance-analysis sections below it — Avatar, Name,
 * Country, and Privacy Trim live on `SettingsPage.tsx` instead (§4.12), a separate screen
 * this page cross-links to rather than duplicating. Upload isn't offered here either, for a
 * related reason — there's nothing profile-specific about uploading, so it stays exactly
 * where it already lives, on the map screen this page is a detour from.
 *
 * `Trends` lives here too, below the grid — the same "look back at what I did" territory,
 * how much ground was covered over recent weeks/months rather than a single day.
 */
export function ProfilePage({ onBack, onOpenSettings }: ProfilePageProps) {
  return (
    <div className="app-shell">
      <Header onBrandClick={onBack} onOpenSettings={onOpenSettings} />
      <main className="profile-page__body">
        <button type="button" className="profile-page__back" onClick={onBack}>
          ← Back to map
        </button>
        <ActivityGraph />
        <Trends />
      </main>
    </div>
  );
}
