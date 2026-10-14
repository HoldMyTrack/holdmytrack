# ADR-0045: Every API error is JSON `{"error": code, "message": text}`

## Status

Accepted.

## Context

The JSON API answered errors in two shapes. Most were plain text: `http.Error` with a message, some of them translated into the request's language and many untranslated developer strings ("invalid request body", "activity not found"). Four were JSON `{"error": code, "message": text}`, where a client had to tell one failure from another on the same status: `email_not_verified` (sent without a message), `demo_read_only`, `photo_needs_place` and `split_hides_part`.

Both clients coped by trying one shape and falling back to the other: the web's `messageFromErrorBody` and Android's `ApiException` read a JSON body's `message` and otherwise take the text as it is. To branch, the web searched the raw text for `"photo_needs_place"`. The API contract (`IMPLEMENTATION.md` §4.31) had to describe both shapes, sometimes on one response: a photo upload's 422 was JSON when it couldn't place the photo and plain text when the image was too large. Adding a code to an existing error would have changed its shape.

## Decision

- **Every error the API sends is `application/json` `{"error": code, "message": text}`**: every `/v1` route, the session, verification and demo checks in front of them, the rate limits, the daily budget, the cross-origin refusal, an unknown `/v1` path, and the tiles under `/tiles/v1`. The HTML pages keep rendering HTML.
- **`error` is a stable snake_case code** for a program to branch on. A client never branches on `message`.
- **`message` is for a person**, in the request's language when the server has the text translated, and always present.
- **A failure with its own translated message gets its own code**, the message key without its `error.` prefix (`error.photo_no_track` is `photo_no_track`). A failure without one gets its status's generic code: `invalid_request`, `unauthenticated`, `forbidden`, `not_found`, `conflict`, `gone`, `payload_too_large`, `unsupported_type`, `unprocessable`, `rate_limited`, `unavailable` or `internal`.
- **The codes clients already branch on keep their names**: `email_not_verified`, `demo_read_only`, `photo_needs_place`, `split_hides_part`.
- **The contract describes one `Error` schema.** A response lists its exact codes only where a client branches on them.

## Alternatives considered

- **Plain text everywhere**, branching on status alone. A photo upload's 422 means either "needs a place" or "image too large", and only a code tells them apart; so would any future pair of failures on one status.
- **The mix, written down**: plain text by default, JSON where a client branches. It costs nothing now, but keeps two parsers in every client and the string search for codes, and an error that later needs a code changes shape under clients that already read it.
- **RFC 9457 Problem Details** (`application/problem+json`: `type`, `title`, `status`, `detail`, plus a code of our own). The standard for a public API with third-party consumers, which HoldMyTrack doesn't have (`VISION.md` §1.1). It wants a URI per error type, splits the text between `title` and `detail` awkwardly for translated messages, repeats the HTTP status, and would have changed the shape of the four errors clients already read.
- **An explicit code at every call site** rather than one derived from the message key. Clearer at each site, but some 250 of them, most of which have no client branching on them; the key already names the failure.

## Consequences

- **Clients parse one shape** and branch on `error`. The web's and Android's existing parsers already read JSON's `message` first, so app versions already installed show the same text when the server switches: nothing breaks for them.
- **Every error gets a code at once**, so a client that later needs to tell two failures apart can branch without a server change, if the failure has its own message key.
- **Renaming a message key renames its code.** A key a client branches on is a wire name from then on; the contract lists those codes, and their tests catch a rename.
- **Untranslated developer strings stay English** under a generic code. Translating them is a separate change and doesn't touch the shape.
- **Not covered:** the Go mux's own 405 for a known `/v1` path with the wrong method, which no client sends, and `/healthz`, which only the deployment's health check reads. The OAuth round trips end in redirects, whose `?error=` already carries a code.