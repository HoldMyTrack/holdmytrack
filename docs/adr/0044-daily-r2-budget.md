# ADR-0044: A daily R2 budget the app enforces itself, giving up renders, then adding, then the map's reads

## Status

Accepted. Built: `internal/budget` counts every object store request each process makes and holds the day's spend to `R2_DAILY_BUDGET_USD` ($2 by default) (`SPEC.md` FR-17).

## Context

The per-account limits (ADR-0043) and the rate limits bound what one account can cost, and the signup limit how many accounts one address makes. Nothing bounded what every account together costs in a day: a few hundred accounts, each inside its own limits, or a burst of real use after a mention somewhere, could run up R2's per-request bill (Class A writes and listings at $4.50 a million, Class B reads at $0.36) with no matching income (`VISION.md` §4.3). Cloudflare sends billing notifications but has no spending cap on R2, and nothing in the app counted its requests.

Every request to the app's bucket goes through one client (`internal/storage`), so the app can count them as they're made. The basemap can't be: browsers read it from its own public bucket, and the app never sees those reads.

## Decision

- **Count every request on the wire**, in each process, by the class R2 bills it as, and add the counts to the UTC day's total in Postgres every 30 seconds. Every process reads the total back, so the budget is the deployment's, not one process's.
- **Price it at list prices, without the free monthly allowance**, so the spend counted is never less than the bill's.
- **A daily budget in dollars**, `R2_DAILY_BUDGET_USD`, $2 by default: a day's worst case of about $60 a month, many times today's use.
- **Give things up in stages, before the budget, not at it**: at 70% renders wait for the next day; at 85% anything that adds objects is refused (uploads, syncs, photos, avatars, accepting a Story copy, asking for a data download, and building one); at 100% the reads the map makes of the bucket are refused (Fog and Heatmap tiles, photo images, avatars, download archives, the track editor's points). The order is by what can wait without loss: a render catches up the next day, a refused upload can be sent again, and what everyone sees goes last. Starting below 100% keeps the day's spend close to the budget, since what still runs at each stage keeps costing.
- **Everything the database alone serves keeps working at every stage**: the track tiles, the list, Stories, totals, Spots and every page.
- **Refused with a 503 and a Retry-After to midnight UTC**, with a message saying it's paused until then and nothing is lost. The day resets at midnight UTC.

## Alternatives considered

- **Only Cloudflare's billing notifications.** They tell someone after the money is spent, and nothing in the app can act on them.
- **A budget per month.** A bad day would use up the month, and stop everything for the rest of it; a day's budget bounds the damage to a day.
- **One switch at 100%: everything that touches the bucket stops.** Simpler, but the map's Fog and Heatmap would go blank to save renders that could simply wait.
- **Counting at each call site** (each Put and Get). Misses what the client library does underneath — a listing's pages, a multipart upload's parts, a resumed download's range reads — which the bill counts. The transport sees each request.
- **Counting the basemap through a proxy in the app.** Would put every map tile of every visitor through the droplet, the cost the basemap's public bucket and Cloudflare's cache are there to avoid. The basemap is protected at the edge instead (`docs/DEPLOY.md`'s Cloudflare rules) and by serving it as cacheable tiles (`ROADMAP.md`, Milestone 2's Cost control).

## Consequences

- **A day's request bill has a ceiling**, give or take what runs between a 30-second flush and the next read of it, and what the last stage still allows.
- **On a bad day the service degrades for everyone**, real users included, until midnight UTC. That's the trade: a bounded bill over an unbounded one. The alerts at 50% and 70% are there so someone can look first, and raise the budget if the day's use is real.
- **The basemap is outside it**, and stays the most likely cost of a viral day.
- **Requests from a one-off subcommand count too**, added when it finishes; the backups' `rclone` requests don't, being outside the app.
- **Storage itself isn't counted**: it grows slowly and is bounded per account by ADR-0043.