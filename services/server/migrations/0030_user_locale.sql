-- The account's language (IMPLEMENTATION.md §4.21, SPEC.md FR-13, ADR-0014): an
-- internal/i18n catalog code ('en', 'ru'). NULL, every existing row, means "automatic": each
-- page, API message and email follows the language the browser asks for (Accept-Language),
-- else English — what every account got before languages existed, so no backfill.
ALTER TABLE users ADD COLUMN locale VARCHAR(8);
