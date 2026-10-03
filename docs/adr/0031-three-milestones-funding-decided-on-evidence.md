# ADR-0031: Three milestones (MVP, community, social graph), with the long-term funding model decided on evidence from the second; "no data sales" is the one standing promise

## Status

Accepted. Applied to `VISION.md` §1, §5, §6, §8, `ROADMAP.md`, `AGENTS.md`, and the About page and home-page description.

## Context

HoldMyTrack is free for everyone and paid for by community donations. The documents went further than that: they promised it would stay free with "no ads, no data sales, no tiers", ruled out ever charging for a feature, and kept a social graph unscheduled until donations could fund moderation. That locked in a funding model before a single donation had come in, and left social with no real path to being built, since a donation base large enough to pay a trust-and-safety team was unlikely to appear on its own.

Social features change the cost model: their cost is staff time for moderation, abuse handling and safety, which donations scale to worst (`VISION.md` §5.8). Whether that cost is worth taking on, and how to pay it, depends on numbers nobody has yet: how many people use HoldMyTrack, what they ask for, and what share of them give.

## Decision

**The product moves through three milestones.**

1. **MVP**, where HoldMyTrack is now: activities, the map modes, Spots, Stories and photos, on the web and Android. Free for everyone, community-funded.
2. **Release and community.** A public launch, then settling: polish, bug fixes, and adjusting features to what users actually ask for. Its entry gate is what a public launch needs anyway: the funding page, a privacy policy and a DPIA. Its exit gate is a measured answer to what community funding can carry: monthly donations against the monthly bill, and the share of active users who give, over several months.
3. **Social graph.** Public pages, followers and the rest. Whether to build it, and how to pay for it, is decided from Milestone 2's numbers: paid features, community funding, or sponsors. Not building it at all, and staying a single-player product, is one of the options and an acceptable outcome.

**No promise about the future funding model.** HoldMyTrack doesn't say it will be free forever, and doesn't say it will never carry ads. What it says is true today: it's free, every feature is available to everyone, and donations pay for it.

**One standing promise: user data is never sold.** A fog map is a precise record of where someone lives; whether it's for sale is the first question a user asks, and silence reads as "yes". Selling precise location history would need each user's explicit consent under GDPR anyway, so the promise costs almost nothing to keep.

## Alternatives considered

- **Keep the "free forever, no ads, no tiers" commitment.** Rejected: it decides the funding model before there's any evidence about it, and leaves social waiting on donations that may never reach a team's salary.
- **Plan a paid tier now.** Rejected: there is nothing yet to show what users would pay for, and a paywall before a community exists would cost the launch its best line.
- **Drop every promise, data sales included.** Rejected: the promise is nearly free to keep, and for location data its absence is a warning sign.
- **Keep social permanently out of scope.** Kept as an option at the Milestone 3 decision, not taken now: it's better decided with Milestone 2's numbers in hand.

## Consequences

- The funding model can change at Milestone 3 without breaking a promise. The cost is a weaker pitch today: "free, funded by its users" in place of "free forever, no ads".
- Milestone 2 has to produce numbers, not impressions: cost per active user and donation share are what the Milestone 3 decision is made from (`ROADMAP.md`).
- The privacy policy, when it's written, must say that user data is never sold.
- If Milestone 3 brings in a paid feature, that's a new ADR that changes `VISION.md` §6.2, which today says every feature is free for everyone.