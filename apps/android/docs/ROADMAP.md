# HoldMyTrack for Android — Roadmap

The Android app's open work: testing and releasing it on Google Play. The product's is root `docs/ROADMAP.md`.

## Ongoing

- [ ] **Testing tracks, then production**
  - Version 896 (`0.9`) is on the internal and closed testing tracks; Google sign-in against Play's key was checked on the internal track's earlier build. The Play Console account is personal, so it can't publish to production until a closed test has had at least 12 testers opted in continuously for 14 days (Play Console Help, "App testing requirements for new personal developer accounts"). Next: recruit those testers through Testers Community and read the pre-launch report. Testers are pointed at `https://holdmytrack.com/testing`, the step-by-step test suite with its sample files (root `docs/SPEC.md` FR-10.7). Then apply for production access and release in stages, after root `ROADMAP.md`'s "Before launch", since a production release is a public launch.
  - Recheck the release requirements against Play Console Help before each submission.
- [ ] **The Digital Services Act trader-status declaration**, which the Play Console account needs before the app can be distributed in the EU.
- [ ] **No payment link reachable from the Play build, once donations open**
  - Play's Payments policy lets an app point to payment only through Google Play's billing, so the Play build has no Donate (`apps/android/docs/IMPLEMENTATION.md` §6). Help, Contacts and Privacy open the web's pages in a browser tab, whose header's Donate goes to Open Collective once `OpenCollectiveSlug` (`services/server/internal/web/web.go`) is set, and About's funding section would link there too. StreetComplete's Play submission was rejected for such a link; recheck before each submission once donations open.
- [ ] **`WebPageActivity.route` under test**
  - It takes an Android `Uri`, so it has no unit test, though the first-run redirect bug lived in it. Move the decision into a function over plain values and test it (`AGENTS.md`, "Code and tests").