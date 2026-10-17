# HoldMyTrack: Data Protection Impact Assessment

Last updated: 2026-10-17.

## Summary

HoldMyTrack can launch publicly once the five pre-launch actions in section 7 are done. No risk stays high after the measures in place, so prior consultation with a supervisory authority (GDPR Art. 36) is not needed. Four stay medium (section 6).

**Status:** draft for the controller's review, written from the product's documents and its published privacy policy (`/privacy`, last updated 17 October 2026), without an audit of the running system.

**Controller:** open question. HoldMyTrack runs at holdmytrack.com, contact hello@holdmytrack.com. The operator lives outside the EU; GDPR applies to a non-EU controller serving EU users (Art. 3(2)), who appoints an EU representative (Art. 27). The privacy policy names only "the HoldMyTrack project" (section 7).

**What is assessed:** the web app, the server and its storage, and the Android app as they ship, with all the data in section 2. Planned cloud connectors and the iOS app are out of scope (section 7, review triggers).

## 1. Why a DPIA is needed

A DPIA is required: the processing meets two of the nine criteria in the EDPB's DPIA guidelines (WP248 rev.01), and two are usually enough.

| WP248 criterion | Met? | Why |
| --- | --- | --- |
| Systematic monitoring | Yes | A user's tracks, kept over years, form a record of their movements. |
| Sensitive or highly personal data | Yes | Precise location reveals home, work and routines. Not an Article 9 special category; no health data is processed. |
| Large scale | Not yet | Few accounts before launch; revisit as it grows. |
| Matching or combining datasets | Partly | Tracks from several sources are combined, for the same purpose and controller. |
| Vulnerable data subjects | No | Not for children under 16. |

Innovative technology, evaluation or scoring, automated decisions with legal effect, and preventing a right or a service don't apply.

The ICO and the CNIL also list location tracking as needing one.

## 2. The processing

HoldMyTrack keeps the outdoor activities a user chooses to bring in and draws them as private maps and stats (one map of everywhere they went, Fog of War, Stories and Spots), shown to no one but the account holder.

**Purposes:** run the service the user signed up for and keep it working and secure. No advertising, analytics, profiling or data sale. To measure cost per active user and which clients are still in use, it keeps daily counts across all accounts (`IMPLEMENTATION.md` §4.32), which describe no one.

**Data subjects:** registered users aged 16 and over. Third parties appear only incidentally, as faces in photos or a Story copy recipient's email address.

**How data comes in** (each opt-in, section 3):

1. File upload on the web or in the app (GPX, FIT, TCX).
2. Health Connect on Android: exercise sessions and their routes, read only while the Sync screen is open.
3. In-app GPS recording on Android, kept on the phone until the user syncs it.
4. A Story copy another user sends, added only once the recipient accepts it.

**What happens on the server.** Private locations are applied when an activity is processed, before tracks, tiles or stats are stored. The original upload is kept unclipped so tracks can be rebuilt after an edit or a Private location change.

### Data inventory

| Data | Source | Stored at | Kept for |
| --- | --- | --- | --- |
| Email, password hash, Google/Facebook id and name | User, sign-in provider | Database (DigitalOcean, US) | Until the account is deleted |
| Display name, avatar, country, timezone, language | User | Database; avatar image in R2 | Until the account is deleted |
| Activity tracks: points, times, elevation, derived stats | Uploads, Health Connect, recording | Database, map tiles in R2 | Until the activity or account is deleted |
| Original uploads, unclipped, points inside Private locations included | Uploads, Health Connect, recording | Cloudflare R2 | Until the activity or account is deleted; no expiry yet |
| Private locations (circles around home, work) | User | Database | Until deleted |
| Stories, names, descriptions | User | Database | Until deleted |
| Photos, resized and stripped of EXIF | User | Cloudflare R2 | Until deleted |
| Spot captures (place and time, not position) | Android app | Database | Until deleted |
| Import records | System | Database | Until the account is deleted |
| The day an account last used the service, the website and the Android app, and the app's version | System | Database | Until the account is deleted |
| Server logs with account ids, no tracks or emails | System | Server, then Grafana Cloud (US) | Rotated on the server; 14 days at Grafana |
| Daily service statistics: counts, sums and percentiles across all accounts, never one account's; country groups under 5 accounts folded into one | System | Database; the newest day also at Grafana Cloud (US) | Indefinitely; 14 days at Grafana |
| Nightly backups: database dump, originals, photos, avatars | System | Cloudflare R2 | Up to 8 weeks after deletion; newest 3 dumps also on the server |
| Data download archive | User's request | Cloudflare R2 | 7 days |

**Who can see it:**

- **The user:** all of their own data.
- **A Story recipient:** the copy they accepted, with the sender's Private locations applied, theirs from then on.
- **The operator:** through the admin panel, every account's list of activities (dates, names, countries), but not their tracks.
- **Processors:** DigitalOcean (servers), Cloudflare (R2 storage), Grafana Labs (monitoring) and an email delivery provider that isn't named yet.

Google Fonts, MapTiler (Satellite only) and Google/Facebook sign-in receive the user's IP address from their browser directly.

## 3. Legal basis, necessity and proportionality

| Processing | Lawful basis (GDPR Art. 6) |
| --- | --- |
| Account, activities, Private locations, Stories, photos, captures | (1)(b) contract: the service the user signed up for |
| Reading Health Connect and the phone's location | (1)(a) consent, withdrawable in Android settings or Health Connect |
| Server logs, backups, sign-in rate limits | (1)(f) legitimate interest in a working, secure service |
| Account emails (confirm address, reset password, data download, Story copy) | (1)(b) contract |

No Article 9 special-category data is processed. Heart rate and other body data inside an uploaded file is never read out of it (ADR-0017).

**One inconsistency to resolve:** `VISION.md` §7 names consent as the basis for location history; the privacy policy relies on contract, which fits better, since the map can't be drawn without the track (section 7).

**Data minimisation in place:**

- **Only what the user ticks comes in.** No source starts ticked, and nothing is imported in bulk (ADR-0039).
- **No background location or continuous location log.** Recording, Health Connect reads and Spot captures happen only in the foreground.
- **No health data.** Only exercise sessions and their routes are read from Health Connect, one declared data type.
- **Photos are stored stripped.** They are re-encoded before upload, so no EXIF data, position included, reaches the server.
- **A Spot capture sends the place and the time,** not the user's position.
- **Logs leave the server scrubbed.** Tracks, places, names, emails and IP addresses are removed first.

**The one retention beyond what the map needs** is the original upload (section 2), with the points inside Private locations (R2).

## 4. Data subjects' rights

Users exercise most rights in the product, and the rest by email to hello@holdmytrack.com, answered within a month.

| Right | How it is met |
| --- | --- |
| Information (Art. 13) | Privacy policy at `/privacy`, linked from the site, the app and the Health Connect rationale |
| Access and portability (Art. 15, 20) | Settings › Download your data: originals, GPX tracks, photos and `account.json` |
| Rectification (Art. 16) | Settings and the Edit window; anything else by email |
| Erasure (Art. 17) | Settings › Delete account on the web and in the app, gone within minutes; by email if the user cannot sign in. Backups hold it for up to 8 weeks. |
| Restriction and objection (Art. 18, 21) | By email |
| Withdrawing consent (Art. 7(3)) | Android settings or Health Connect; data already sent stays until the user deletes it |
| Complaint to an authority (Art. 77) | Stated in the privacy policy |

**Story copies limit erasure:** an accepted copy is the recipient's, so deleting the sender's account leaves it (section 2, R6).

## 5. Risks to individuals

Risks to users, rated before the measures in section 6 on the ICO template's scales.

| # | Risk | Likelihood | Severity | Overall |
| --- | --- | --- | --- | --- |
| R1 | A breach of the server, database or storage exposes users' tracks, revealing home, work and routines | Possible | Severe | High |
| R2 | The original, unclipped uploads leak, exposing points inside Private locations that users chose to hide | Possible | Severe | High |
| R3 | A software bug shows one user's tracks or tiles to another | Possible | Severe | High |
| R4 | Someone takes over an account and reads its history | Possible | Significant | Medium |
| R5 | The operator, or someone with operator access, looks at a user's data without need | Remote | Significant | Low |
| R6 | An exported map image, or a Story copy sent to someone, reveals more than the user meant | Possible | Significant | Medium |
| R7 | Data of EU users stored in the US is accessed by US authorities | Remote | Significant | Medium |
| R8 | A processor misuses or mishandles data, or one is used without a data processing agreement | Remote | Significant | Medium |
| R9 | Data is kept long after it is needed, for example in a dormant account, increasing what any breach exposes | Probable | Significant | Medium |

## 6. Measures and residual risk

R7 and R8 stay medium until the actions in section 7 are done. R2 and R9 stay medium by decision: HoldMyTrack keeps what a user brings until they delete it, with no dormancy policy, and keeps original uploads (section 3).

| # | Measures in place | Still missing | Residual |
| --- | --- | --- | --- |
| R1 | Private locations applied at ingest, before tracks or tiles are stored (ADR-0002). TLS everywhere through Caddy. Separate R2 token for backups, with no access to the app bucket. No public profiles, so there is nothing to scrape. | Confirmation that the VPS disk, database and both R2 buckets are encrypted at rest, and who holds server access | Low |
| R2 | Originals sit in a private R2 bucket, never publicly addressable. Exports and Story copies are built from clipped tracks, never from originals. | A decision on how long originals are kept, after launch (`docs/ROADMAP.md`, "After launch"); a check that the bucket has no public access | Medium |
| R3 | The tile and API endpoints take the user from the session, never from a parameter. Server tests check handlers against the API contract. | A test that one account cannot read another's activities, tiles or photos, if none exists | Low |
| R4 | bcrypt password hashes. HttpOnly, SameSite=Lax session cookie for up to 30 days. Sign-in rate limits. Email verification. | Optional second factor; not needed for launch | Low |
| R5 | The admin panel never shows tracks (section 2). Admin rights can be set only from the server's own command line. The daily statistics hold only aggregates (section 2). | An audit log, needed once an admin can see or change a track | Low |
| R6 | Exports leave out Private locations and draw only clipped tracks, whose ends stop a secret, random distance past each circle so many tracks together don't outline it. A Story copy needs the recipient to accept it and carries the sender's Private locations. The policy warns that a copy is the recipient's. | — | Low |
| R7 | Transfers rely on the processors' Standard Contractual Clauses or EU–US Data Privacy Framework certification, stated in the privacy policy. | EU-region hosting for EU users (`docs/ROADMAP.md`, "Before launch"), or a transfer impact assessment that justifies staying in the US | Medium |
| R8 | Processors are named in the privacy policy. Logs are scrubbed before they reach Grafana (section 3). | Signed or accepted DPAs with DigitalOcean, Cloudflare and Grafana Labs on file; the email provider chosen, named and under a DPA | Medium |
| R9 | Deletion is complete within minutes and reaches backups within 8 weeks; the rest expires as section 2 lists. | — | Medium |

## 7. Actions and sign-off

**Before launch:**

- [ ] Decide who the controller is: the operator personally, with an EU representative (Art. 27); the Open Collective fiscal host, if it agrees to take the role; or a legal entity set up for the project. Then name it, with a postal address and any EU representative, in the privacy policy's "Who we are".
- [ ] Have DPAs on file with DigitalOcean, Cloudflare and Grafana Labs (R8).
- [ ] Choose the email delivery provider, accept its DPA, and name it in the privacy policy (R8).
- [ ] Decide on EU-region hosting for EU users, or write a transfer impact assessment that justifies the US (R7).
- [ ] Confirm encryption at rest on the VPS disk, the database and both R2 buckets, and that the app bucket has no public access (R1, R2).

**Soon after:**

- [ ] Review how long original uploads are kept (R2; `docs/ROADMAP.md`, "After launch").
- [ ] Add or confirm a test that one account cannot read another's activities, tiles or photos (R3).
- [ ] Re-check that the cookies and browser storage the site sets still match the Cookies list on `/privacy`.
- [ ] Align `VISION.md` §7 with the privacy policy's lawful basis (section 3).

**Review this DPIA** before a cloud connector or the iOS app ships, when an admin can see tracks, or when the user base becomes large-scale. At large scale, systematic monitoring as a core activity also makes a Data Protection Officer mandatory (Art. 37(1)(b)).

### Sign-off

| Role | Name | Decision | Date |
| --- | --- | --- | --- |
| Controller | Open question | | |
| Reviewer with GDPR experience (recommended) | | | |