# HoldMyTrack: Data Protection Impact Assessment

Last updated: 2026-10-13.

## Summary

HoldMyTrack can launch publicly once the five pre-launch actions in section 7 are done. No risk stays high after the measures in place, so prior consultation with a supervisory authority (GDPR Art. 36) is not needed. Four risks stay medium: two from gaps still to close, no EU-region hosting and processor agreements not yet on file, and two by decision, since original uploads and dormant accounts' data are kept until the user deletes them.

**Status:** draft for the controller's review. It was written from the product's own documents and its published privacy policy (`/privacy`, last updated 12 October 2026), not from an audit of the running system.

**Controller:** open question. HoldMyTrack runs at holdmytrack.com, contact hello@holdmytrack.com. The operator lives outside the EU, which does not rule them out: GDPR applies to a non-EU controller serving EU users (Art. 3(2)), who then appoints an EU representative (Art. 27). The privacy policy names only "the HoldMyTrack project" so far (section 7).

**What is assessed:** the web app, the server and its storage, and the Android app as they ship today. That covers account data, activity tracks, Private locations, Stories and Story copies, photos, Spot captures, Health Connect sync, in-app GPS recording, the admin panel, logs and backups. Planned cloud connectors (Wahoo, COROS) and the iOS app are out of scope; each needs this assessment updated before it ships.

## 1. Why a DPIA is needed

A DPIA is required: the processing clearly meets two of the nine criteria in the EDPB's DPIA guidelines (WP248 rev.01), and two are usually enough.

| WP248 criterion | Met? | Why |
| --- | --- | --- |
| Systematic monitoring | Yes | A user's tracks, kept over years, form a record of their movements. |
| Sensitive or highly personal data | Yes | Precise location reveals home, work and routines. It is not an Article 9 special category, and no health data is processed. |
| Large scale | Not yet | Few accounts before launch. Revisit once the user base grows. |
| Matching or combining datasets | Partly | Tracks from several sources (files, Health Connect, in-app recording, Story copies) are combined, but all for the same purpose and controller, so this is borderline. |
| Vulnerable data subjects | No | The service is not for children under 16. |
| Innovative technology | No | GPS tracks and map tiles are established technology. |
| Evaluation or scoring | No | No profiling, ads or scoring. Stats only describe the user's own tracks. |
| Automated decisions with legal effect | No | None. |
| Prevents exercising a right or using a service | No | None. |

Several national authorities, including the UK ICO and the French CNIL, also list tracking a person's location on their own lists of processing that needs a DPIA.

## 2. The processing

HoldMyTrack keeps the outdoor activities a user chooses to bring in and draws them as private maps and stats: one map of everywhere they went, Fog of War, Stories and Spots. Nothing is shown to anyone but the account holder.

**Purposes:** run the service the user signed up for, and keep it working and secure. There is no advertising, analytics, profiling or data sale. To measure what the service costs per active user, it keeps a daily count of its own data across all accounts (`IMPLEMENTATION.md` §4.32), which describes no one and is built only from data it already holds to run.

**Data subjects:** registered users aged 16 and over. Third parties appear only incidentally, as faces in a user's photos or as the recipient's email address when a Story copy is sent.

**How data comes in.** Each source is opt-in, and only what the user ticks comes in:

1. File upload on the web or in the app (GPX, FIT, TCX).
2. Health Connect on Android: exercise sessions and their routes, read only while the Sync screen is open.
3. In-app GPS recording on Android, kept on the phone until the user syncs it.
4. A Story copy another user sends, added only once the recipient accepts it.

**What happens on the server.** Private locations are applied when an activity is processed, before tracks, tiles or stats are stored. The original upload is kept unclipped so that tracks can be rebuilt when the user edits them or changes a Private location.

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
| Server logs with account ids, no tracks or emails | System | Server, then Grafana Cloud (US) | Rotated on the server; 14 days at Grafana |
| Daily service statistics: counts, sums and percentiles across all accounts, never one account's; country groups under 5 accounts folded into one | System | Database; the newest day also at Grafana Cloud (US) | Indefinitely; 14 days at Grafana |
| Nightly backups: database dump, originals, photos, avatars | System | Cloudflare R2 | Up to 8 weeks after deletion; newest 3 dumps also on the server |
| Data download archive | User's request | Cloudflare R2 | 7 days |

**Who can see it:**

- **The user:** all of their own data.
- **A Story recipient:** the copy they accepted, with the sender's Private locations already applied. The copy is theirs from then on.
- **The operator:** through the admin panel, every account's list of activities (dates, names, countries), but not their tracks.
- **Processors:** DigitalOcean (servers), Cloudflare (R2 storage), Grafana Labs (monitoring) and an email delivery provider that isn't named yet.

Google Fonts, MapTiler (Satellite only) and Google/Facebook sign-in receive the user's IP address from their browser directly.

## 3. Legal basis, necessity and proportionality

Every category of data has a lawful basis, and each is limited to what drawing the user's own map needs.

| Processing | Lawful basis (GDPR Art. 6) |
| --- | --- |
| Account, activities, Private locations, Stories, photos, captures | (1)(b) contract: the service the user signed up for |
| Reading Health Connect and the phone's location | (1)(a) consent, withdrawable in Android settings or Health Connect |
| Server logs, backups, sign-in rate limits | (1)(f) legitimate interest in a working, secure service |
| Account emails (confirm address, reset password, data download, Story copy) | (1)(b) contract |

No Article 9 special-category data is processed. Heart rate and other body data that sits inside an uploaded file is never read out of it (ADR-0017).

**One inconsistency to resolve:** `VISION.md` §7 names explicit consent as the legal basis for location history, while the privacy policy relies on contract. Contract is the better fit, because the map cannot be drawn without the track, and consent would be harder to keep valid. `VISION.md` should be brought in line.

**Data minimisation in place:**

- **Only what the user ticks comes in.** No source starts ticked, and nothing is imported in bulk (ADR-0039).
- **No background location, and no continuous location log.** Recording, Health Connect reads and Spot captures happen only in the foreground.
- **No health data.** Only exercise sessions and their routes are read from Health Connect, one declared data type.
- **Photos are stored stripped.** They are re-encoded before upload, so no EXIF data, position included, reaches the server.
- **A Spot capture sends the place and the time,** not the user's position.
- **Logs leave the server scrubbed.** Tracks, places, names, emails and IP addresses are removed first.
- **No analytics, advertising or tracking pixels.**

**The one retention that goes beyond what the map needs** is the original, unclipped upload. It exists so that tracks can be rebuilt after an edit or a Private location change. That is a real user benefit, but it means points inside Private locations stay stored, which is the main risk in section 5. How long they are kept is to be reviewed soon after the public launch (`docs/ROADMAP.md`, "Ongoing").

## 4. Data subjects' rights

Users can exercise most rights themselves in the product, and the rest by email to hello@holdmytrack.com, answered within a month.

| Right | How it is met |
| --- | --- |
| Information (Art. 13) | Privacy policy at `/privacy`, linked from the site, the app and the Health Connect rationale |
| Access and portability (Art. 15, 20) | Settings › Download your data: originals, GPX tracks, photos and `account.json`, emailed when ready and kept for 7 days |
| Rectification (Art. 16) | Settings and the Edit window; anything else by email |
| Erasure (Art. 17) | Settings › Delete account on the web and in the app, gone within minutes; by email if the user cannot sign in. Backups hold it for up to 8 weeks. |
| Restriction and objection (Art. 18, 21) | By email |
| Withdrawing consent (Art. 7(3)) | Android settings or Health Connect; data already sent stays until the user deletes it |
| Complaint to an authority (Art. 77) | Stated in the privacy policy |

**Story copies limit erasure, by design.** A copy a recipient accepted becomes the recipient's data, so deleting the sender's account does not remove it. The privacy policy says so before anything is sent, and the copy already has the sender's Private locations applied.

## 5. Risks to individuals

The gravest risk is that a user's location history is exposed and reveals where they live and when they are away. These are the risks to the users, not to the project, rated before the measures in section 6.

The ratings follow the ICO template. **Likelihood** is remote, possible or probable. **Severity** is minimal, significant or severe. **Overall** is low, medium or high.

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

After the measures already in place, R1 and R3–R6 drop to low. R7 and R8 stay medium until the actions in section 7 are done. R2 and R9 stay medium by decision: HoldMyTrack keeps what a user brings until they delete it, with no dormancy policy, and keeps original uploads so that tracks can be rebuilt (section 3).

| # | Measures in place | Still missing | Residual |
| --- | --- | --- | --- |
| R1 | Private locations applied at ingest, before tracks or tiles are stored (ADR-0002). TLS everywhere through Caddy. Separate R2 token for backups, with no access to the app bucket. No public profiles, so there is nothing to scrape. | Confirmation that the VPS disk, database and both R2 buckets are encrypted at rest, and who holds server access | Low |
| R2 | Originals sit in a private R2 bucket, never publicly addressable. Exports and Story copies are built from clipped tracks, never from originals. | A decision on how long originals are kept, after launch (`docs/ROADMAP.md`, "Ongoing"); a check that the bucket has no public access | Medium |
| R3 | The tile and API endpoints take the user from the session, never from a parameter. Server tests check handlers against the API contract. | A test that asserts one account cannot read another's activities, tiles or photos, if one does not exist | Low |
| R4 | bcrypt password hashes. HttpOnly, SameSite=Lax session cookie for up to 30 days. Sign-in rate limits. Email verification. Google and Facebook sign-in. | Optional second factor; not needed for launch | Low |
| R5 | The admin panel shows activity lists, never tracks. Admin rights can be set only from the server's own command line. The daily statistics hold no account-level rows, and a country group under 5 accounts is folded into one, so no stored number points at one person. | An audit log, needed once an admin can see or change a track | Low |
| R6 | Exports leave out Private locations and draw only clipped tracks, whose ends stop a secret, random distance past each circle so many tracks together don't outline it. A Story copy needs the recipient to accept it and carries the sender's Private locations. The policy warns that a copy is the recipient's. | — | Low |
| R7 | Transfers rely on the processors' Standard Contractual Clauses or EU–US Data Privacy Framework certification, stated in the privacy policy. | EU-region hosting for EU users (`docs/ROADMAP.md`, "Before launch"), or a transfer impact assessment that justifies staying in the US | Medium |
| R8 | Processors are named in the privacy policy. Logs are scrubbed of tracks, places, names, emails and IP addresses before they reach Grafana. | Signed or accepted DPAs with DigitalOcean, Cloudflare and Grafana Labs on file; the email provider chosen, named and under a DPA | Medium |
| R9 | Deletion is complete within minutes and reaches backups within 8 weeks. Download archives expire after 7 days. Grafana logs expire after 14 days. | — (no dormancy policy: an account's data is kept until its owner deletes it, `VISION.md` §4.3) | Medium |

No residual risk is high, so prior consultation with a supervisory authority (Art. 36) is not required.

## 7. Actions and sign-off

Five actions gate the public launch; the rest can follow it.

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

**Review this DPIA** when a cloud connector or the iOS app ships, when an admin can see tracks, or when the user base becomes large-scale. At large scale, systematic monitoring as a core activity also makes a Data Protection Officer mandatory (Art. 37(1)(b)).

### Sign-off

| Role | Name | Decision | Date |
| --- | --- | --- | --- |
| Controller | Open question | | |
| Reviewer with GDPR experience (recommended) | | | |