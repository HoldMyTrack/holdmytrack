import { useEffect, useState } from 'react';
import { API_BASE_URL } from '../api';
import { APP_VERSION } from '../version';

const POLL_INTERVAL_MS = 5 * 60 * 1000;

/**
 * Detects a tab left open across a deploy — every prod deploy fully replaces the static
 * build (Vite's hashed chunk filenames aren't additive, so old chunks are simply gone after
 * a rebuild), and a stale tab can hit "failed to fetch dynamically imported module" the next
 * time it lazy-loads something. Polls the existing /healthz endpoint (added for the
 * DB-connectivity check) rather than a dedicated one, comparing its `version` field against
 * this build's own APP_VERSION. Skipped entirely outside a Docker build (APP_VERSION
 * 'dev') — there's no deploy to go stale against in local dev.
 */
export function VersionBanner() {
  const [stale, setStale] = useState(false);

  useEffect(() => {
    if (APP_VERSION === 'dev') return;

    let cancelled = false;
    const check = () => {
      fetch(`${API_BASE_URL}/healthz`)
        .then((res) => (res.ok ? res.json() : null))
        .then((body: { version?: string } | null) => {
          if (!cancelled && body?.version && body.version !== APP_VERSION) setStale(true);
        })
        .catch(() => {}); // a failed check means "unknown", not "stale" — never claim staleness on a network error
    };
    check();
    const interval = setInterval(check, POLL_INTERVAL_MS);
    const onVisible = () => {
      if (document.visibilityState === 'visible') check();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      cancelled = true;
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, []);

  if (!stale) return null;
  return (
    <div className="version-banner" role="status" data-testid="version-banner">
      A new version of FitMap is available.
      <button type="button" className="version-banner__refresh" onClick={() => window.location.reload()}>
        Refresh
      </button>
    </div>
  );
}
