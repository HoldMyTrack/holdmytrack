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
 * later, they are how it is funded at all (docs/VISION.md §6 — recurring community
 * donations with public accounting, no subscription tier, no paywalled feature).
 */
const DONATE_NOTICE: NoticeContent = {
  title: 'Donations aren’t open yet',
  message:
    'Thank you for wanting to support HoldMyTrack — we’re still in development, and there is nothing here to take a donation with yet. When there is, this is where it will live: HoldMyTrack is funded by recurring community donations with public accounting, not by subscriptions.',
};

/** GitHub's Sponsor-button heart (Primer Octicons' `heart-16`, MIT) — an outline in
 *  currentColor, so it takes the button's own text color rather than a separate pink. */
function HeartIcon() {
  return (
    <svg className="donate-button__icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path
        fill="currentColor"
        d="m8 14.25.345.666a.75.75 0 0 1-.69 0l-.008-.004-.018-.01a7.152 7.152 0 0 1-.31-.17 22.055 22.055 0 0 1-3.434-2.414C2.045 10.731 0 8.35 0 5.5 0 2.836 2.086 1 4.25 1 5.797 1 7.153 1.802 8 3.02 8.847 1.802 10.203 1 11.75 1 13.914 1 16 2.836 16 5.5c0 2.85-2.045 5.231-3.885 6.818a22.066 22.066 0 0 1-3.744 2.584l-.018.01-.006.003h-.002ZM4.25 2.5c-1.336 0-2.75 1.164-2.75 3 0 2.15 1.58 4.144 3.365 5.682A20.58 20.58 0 0 0 8 13.393a20.58 20.58 0 0 0 3.135-2.211C12.92 9.644 14.5 7.65 14.5 5.5c0-1.836-1.414-3-2.75-3-1.373 0-2.609.986-3.029 2.456a.749.749 0 0 1-1.442 0C6.859 3.486 5.623 2.5 4.25 2.5Z"
      />
    </svg>
  );
}

export function DonateButton() {
  const [open, setOpen] = useState(false);

  if (OPEN_COLLECTIVE_SLUG) {
    return (
      <a
        className="donate-button"
        data-testid="donate-button"
        aria-label="Donate"
        href={openCollectiveDonateUrl(OPEN_COLLECTIVE_SLUG)}
        target="_blank"
        rel="noopener noreferrer"
        title="Support HoldMyTrack on Open Collective"
      >
        <HeartIcon />
        <span className="donate-button__label">Donate</span>
      </a>
    );
  }

  return (
    <>
      <button
        type="button"
        className="donate-button"
        data-testid="donate-button"
        aria-label="Donate"
        onClick={() => setOpen(true)}
      >
        <HeartIcon />
        <span className="donate-button__label">Donate</span>
      </button>
      {open && <PlaceholderNotice notice={DONATE_NOTICE} onDismiss={() => setOpen(false)} />}
    </>
  );
}
