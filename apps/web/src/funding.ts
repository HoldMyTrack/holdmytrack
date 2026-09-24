/**
 * HoldMyTrack's Open Collective (docs/VISION.md §6.1 — recurring donations with a public
 * ledger), fiscally hosted by Open Source Collective. A code constant, not a VITE_* build
 * value: which collective funds the project is a fact about the project, not about any one
 * deployment.
 *
 * Empty until the collective actually exists and its host has approved it — DonateButton.tsx
 * keeps showing its "not open yet" notice while this is `''` rather than linking to a 404.
 * The intended slug is `holdmytrack` (unclaimed as of 2026-09-24).
 */
export const OPEN_COLLECTIVE_SLUG: string = '';

/** Open Collective's own contribution flow for the collective — tiers, one-off or monthly,
 *  card/PayPal handling all happen there, so nothing payment-related runs in this app. */
export function openCollectiveDonateUrl(slug: string): string {
  return `https://opencollective.com/${encodeURIComponent(slug)}/donate`;
}
