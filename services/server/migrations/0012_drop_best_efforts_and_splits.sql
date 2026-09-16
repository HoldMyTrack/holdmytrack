-- FitMap's scope is narrowed to outdoor GPS tracking and exploration, not performance
-- analysis (VISION.md §1.1) -- best-effort curves (0010) and personal bests (0011) are cut,
-- not deferred. Per-activity pace/heart-rate (activity_streams, track-metrics) and distance
-- trends stay; this drops only the two derived-stat tables nothing else references.
DROP TABLE activity_splits;
DROP TABLE activity_best_efforts;
