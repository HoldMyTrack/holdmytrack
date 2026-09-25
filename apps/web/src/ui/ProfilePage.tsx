import { ActivityGraph } from './ActivityGraph';
import { Trends } from './Trends';

/**
 * §4.8's activity graph at `/profile`, reached from the header's account menu. Just
 * the "ACTIVITY GRID" panel and the performance-analysis sections below it — Avatar, Name,
 * Country, and Timezone live on `SettingsPage.tsx` instead (§4.12), a separate screen
 * this page cross-links to rather than duplicating. Upload isn't offered here either, for a
 * related reason — there's nothing profile-specific about uploading, so it stays exactly
 * where it already lives, on the map screen this page is a detour from.
 *
 * `Trends` lives here too, below the grid — the same "look back at what I did" territory,
 * how much ground was covered over recent weeks/months rather than a single day.
 */
export function ProfilePage() {
  return (
    <div className="app-shell">
      <main className="profile-page__body">
        <a className="profile-page__back" href="/">
          ← Back to map
        </a>
        <ActivityGraph />
        <Trends />
      </main>
    </div>
  );
}
