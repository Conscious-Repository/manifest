# Exact artifact history view recovery — September 25

Artifact workspace view snapshots now retain version number alongside content
hash, and whether provenance details are expanded. Restoration validates the
number/hash pair against immutable history and selects that entry before applying
mode, panel and reading state. This distinguishes an early version from a later
restore with identical bytes but different actor/time. Legacy snapshots without
a version number retain their previous hash-based behavior. Invalid pairs are
not silently rebound to a different history entry.

The Chromium provenance fixture captures view state for version one, appends a
third version with the same hash and a different actor, recreates the workspace
and restores the view. It asserts version one, its original actor and the open
provenance panel. A mismatched number/hash pair returns false. Existing text edit,
compare/save and editor selection/scroll restoration checks pass. This exercises
workspace serialization/restoration; it does not claim physical-device or full
cross-device acceptance. Full-suite/build/live results are in the plan checkpoint.
