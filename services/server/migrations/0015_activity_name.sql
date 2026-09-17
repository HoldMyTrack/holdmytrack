-- Reverses IMPLEMENTATION.md §4.7's earlier "Resolved: no name column" decision, deliberately
-- narrower than what that decision rejected: a user-entered title only, edited the same way
-- activity_type/description already are (PATCH /v1/activities/{id}), shown as the Activities
-- panel row's primary line in place of started_at when set. Nothing parses one out of a source
-- file (GPX's <name>, TCX's <Notes>, FIT's string fields all still go unread) -- an activity
-- has a name only once a person writes one in, same as description.
--
-- VARCHAR(200), a generous single-line title bound -- description already covers "a couple of
-- paragraphs" (maxActivityDescriptionLen, activities.go) for anything longer than a name.
ALTER TABLE activities ADD COLUMN name VARCHAR(200);
