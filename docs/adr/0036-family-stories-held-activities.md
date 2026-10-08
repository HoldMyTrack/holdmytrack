# ADR-0036: A Story can be shared with family members, and its activities are held by every member, so no one can take another member's memories away

## Status

Accepted, not built (`ROADMAP.md` Phase 1, Family Stories). Supersedes ADR-0020's private-only rule for Stories shared with other accounts; ADR-0020's hand-picked membership, Normal-mode view and story-scoped date picker stand.

## Context

A family trip is one trip for everyone who went, but in HoldMyTrack each person keeps their own Story, and often only one of them recorded it: the child who walked the whole trail has nothing on their Fog of War because the parent's watch did the recording. ADR-0020 made Stories private and left sharing to an exported image (`IMPLEMENTATION.md` §4.3.3). The request is to let family members who already have accounts share one Story, with its activities counting for each of them as their own — on Fog of War and Heatmap, and everywhere else an activity counts.

A journey is a lifelong memory. Sharing one with someone must not make it hostage to that person: a partner who walks out, deletes the trip in a temper or deletes their account must not be able to take it out of anyone else's history. That requirement shapes most of what follows. It also makes this the first feature in which one account reads another account's data, and the first in which deleting an account does not delete everything that account created.

## Decision

**A Story can have members.** Any member invites another existing account by its email address; the invitee accepts or declines in the app. The invite endpoint answers the same way whether the address belongs to an account or not, so it can't be used to find out who has one. All members are equal: any of them can invite and edit the Story's name and description, and the person who created it has no extra rights.

**Only the member themselves can end their membership.** A member leaves a Story (a new action next to Delete); nobody can remove anyone else. A Story whose creator has left or deleted their account carries on for the rest, the creator shown as "a former member". A Story is deleted for good only when its last member has left.

**An activity in a family Story is held, not just owned.** It is held by the person who recorded it and by every member of every family Story it is in. For each holder it counts as their own everywhere: the Activities list, the totals and the histogram, Trends and the activity graph, the tracks tiles, Fog of War, Heatmap, the countries and regions visited, and its photos. Each member adds only activities they recorded themselves.

**Letting go of an activity is a release, not a delete.** Deleting a held activity, leaving the Story, or deleting the account removes it from that person's history only. The activity — its row, its streams, its photos and its per-tile masks (`activity_tile_masks`, `IMPLEMENTATION.md` §4.2) — is deleted when its last holder has released it.

**An edit to an activity with more than one holder forks it.** Chop, Split, Crop, a reprocess, or a change to its type or name gives the editor their own edited copy and leaves everyone else holding the original. Nothing is copied until someone edits.

**A mistake can be undone for 24 hours.** The member who added an activity can take it back out of the Story within 24 hours of adding it; after that the other members hold it.

**Your own recording wins.** A held activity that duplicates one the member recorded themselves is hidden from that member's reads, so the same walk isn't counted twice in their totals or their Heatmap.

**Consent is asked once, plainly.** The first time someone adds an activity to a family Story, a confirmation says that the Story's members keep it even if they later remove it, leave, or delete their account.

**Account deletion keeps everything that wasn't shared.** The name, the email address, the sign-in identities and every activity no one else holds are deleted as before (`SPEC.md` FR-1.11). Held activities stay with the other holders, credited to "a former member". Private locations were applied to them at ingest (ADR-0002), so they don't show where the person lives.

**Removal for safety goes through the admin panel.** Someone who needs their tracks out of another person's account — an abusive relationship is the case this exists for — asks, and an admin removes them (ADR-0013). There is no self-service way to take a held activity from someone else.

**No Fog of War or Heatmap per Story.** Each account keeps its one all-time Fog and Heatmap, composited from the masks of every activity it holds. Joining or leaving a Story, adding an activity, a fork and a release mark the tiles the affected activities touch dirty and bump `map_version` for every account whose holdings changed (`IMPLEMENTATION.md` §4.2, §4.2.6).

## Alternatives considered

- **Copying each activity into every member's account when it's shared.** Every existing read would work unchanged, but storage doubles with every member from the first day, and photos with it. Holding with copy-on-write gives each member the same independence and copies only what someone actually edits.
- **Live references that the recorder can delete.** The simplest design, and exactly the one the requirement rules out: whoever recorded the trip could erase it from everyone else's history.
- **Freezing a shared activity against edits.** Protects the others, but stops anyone fixing their own track — a GPS spike, a drive home that wasn't part of the trip. Forking protects the others without that.
- **Tagging who was on each activity, with Stories only as a shortcut.** More precise, but a second way into the same feature. A Story is how people already group a trip, and its member list decides who shares in it.
- **One merged all-time family Fog of War.** Exposes every member's whole history to the others, not one trip they chose to share.
- **Fog of War and Heatmap per Story.** Rejected in ADR-0020 for its storage cost and still rejected: shared activities feed each member's existing composite instead.
- **Invite links.** A link can be forwarded to anyone. An invite by email reaches one known account.
- **Letting the creator remove members.** Gives one person power over everyone else's memories, the thing this decision is built to prevent.
- **Erasing shared activities when their recorder deletes their account.** Keeps account deletion total, at the cost of the other members' histories — the same failure as a live reference, reached through a different button. The consent confirmation and the admin removal path cover the cases where erasure is genuinely needed.

## Consequences

- Every user-facing read changes from "activities this account owns" to "activities this account holds", through one shared helper, the way every read already skips superseded duplicates (`IMPLEMENTATION.md` §4.6). A read that misses it is a bug that hides an activity from someone it belongs to.
- Deleting an activity becomes a reference-counted release everywhere, the account-purge worker included (`IMPLEMENTATION.md` §4.28).
- Editing gains a copy-on-write path for held activities, across every edit the web and the Android app offer.
- Duplicate detection, today within one account, has to compare a held activity against the holder's own recordings.
- A change to a held activity marks tiles dirty for every holder, not one account.
- A holder's own Private locations don't trim activities they hold but didn't record, so a shared track can pass their home and show in their exports (`VISION.md` §7, share scoping).
- Account deletion is no longer total. The privacy policy (`SPEC.md` FR-10.6) and the DPIA (`ROADMAP.md` Phase 6) have to state what stays and on what basis: the other members were there too, and the person consented when sharing.
- "No feature reads another account's data" stops being true, which matters to any future split of accounts across regions (`BRAINSTORM.md`).
- This is not the social graph of Milestone 3 (`VISION.md` §5.7, §5.8): there are no public pages, no feed and no follows, a Story is shared only with people a member invited by email, and nothing live or recent is shared — only activities a member chose to add.