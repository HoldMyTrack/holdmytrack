-- The daily R2 budget (IMPLEMENTATION.md §5.7, docs/SPEC.md FR-17): every process adds the object
-- store requests it made to its UTC day's row every 30 seconds, by the class R2 bills them as
-- ('A' writes and listings, 'B' reads; deletes are free), and reads the day's total back to
-- decide what to stop. One row per day and class, so a day's spend is two rows.
CREATE TABLE r2_ops (
    day    DATE    NOT NULL,
    class  CHAR(1) NOT NULL CHECK (class IN ('A', 'B')),
    count  BIGINT  NOT NULL,
    PRIMARY KEY (day, class)
);
