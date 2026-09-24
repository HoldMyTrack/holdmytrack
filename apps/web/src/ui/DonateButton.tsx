import { useState } from 'react';
import { OPEN_COLLECTIVE_SLUG, openCollectiveDonateUrl } from '../funding';
import { PlaceholderNotice, type NoticeContent } from './PlaceholderNotice';

/**
 * The header's Donate button. With `OPEN_COLLECTIVE_SLUG` set (funding.ts) it is a plain link
 * to that collective's Open Collective contribution page, opened in a new tab so the map
 * behind it isn't lost — the payment itself, and the public ledger it lands in, live entirely
 * on Open Collective. Until then it stays a placeholder that explains donations aren't open.
 *
 * It is in the nav bar either way because donations are not a feature HoldMyTrack adds on
 * later, they are how it is funded at all (docs/VISION.md §5 — recurring community
 * donations with public accounting, no subscription tier, no paywalled feature).
 */
const DONATE_NOTICE: NoticeContent = {
  title: 'Donations aren’t open yet',
  message:
    'Thank you for wanting to support HoldMyTrack — we’re still in development, and there is nothing here to take a donation with yet. When there is, this is where it will live: HoldMyTrack is funded by recurring community donations with public accounting, not by subscriptions.',
};

export function DonateButton() {
  const [open, setOpen] = useState(false);

  if (OPEN_COLLECTIVE_SLUG) {
    return (
      <a
        className="donate-button"
        data-testid="donate-button"
        href={openCollectiveDonateUrl(OPEN_COLLECTIVE_SLUG)}
        target="_blank"
        rel="noopener noreferrer"
        title="Support HoldMyTrack on Open Collective"
      >
        Donate
      </a>
    );
  }

  return (
    <>
      <button type="button" className="donate-button" data-testid="donate-button" onClick={() => setOpen(true)}>
        Donate
      </button>
      {open && <PlaceholderNotice notice={DONATE_NOTICE} onDismiss={() => setOpen(false)} />}
    </>
  );
}
