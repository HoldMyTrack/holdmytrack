import { useState } from 'react';
import { PlaceholderNotice, type NoticeContent } from './PlaceholderNotice';

/**
 * The header's Donate button. A placeholder: no payment integration, no navigation, no link
 * out — clicking it explains that and nothing else happens.
 *
 * It is in the nav bar this early anyway because donations are not a feature FitMap adds on
 * later, they are how it is funded at all (docs/VISION.md §5 — recurring community
 * donations with public accounting, no subscription tier, no paywalled feature). Wiring one
 * up is real work that isn't in scope here; saying so plainly is not.
 */
const DONATE_NOTICE: NoticeContent = {
  title: 'Donations aren’t open yet',
  message:
    'Thank you for wanting to support FitMap — we’re still in development, and there is nothing here to take a donation with yet. When there is, this is where it will live: FitMap is funded by recurring community donations with public accounting, not by subscriptions.',
};

export function DonateButton() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button type="button" className="donate-button" data-testid="donate-button" onClick={() => setOpen(true)}>
        Donate
      </button>
      {open && <PlaceholderNotice notice={DONATE_NOTICE} onDismiss={() => setOpen(false)} />}
    </>
  );
}
