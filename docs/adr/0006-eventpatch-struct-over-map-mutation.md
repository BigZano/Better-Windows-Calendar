# EventPatch struct replaces map[string]any for event mutation

`UpdateEvent(id int64, patch EventPatch)` replaces the previous `UpdateEvent(id int64, fields map[string]any)` pattern.

`EventPatch` is a struct where every field is a pointer. A nil pointer means "leave this field untouched." A non-nil pointer means "set this field to the pointed-to value." The entire patch lands in a single SQL `UPDATE` statement inside one transaction.

The deciding factor is **atomicity**, not performance. The sync engine routinely updates 6–10 fields per event after pulling from a remote (Graph or CalDAV). With the map pattern, a crash or network drop mid-update leaves the event row in a corrupt intermediate state. With `EventPatch` in a single transaction, all fields change together or none do. For sync — where the local store must exactly reflect a known remote state — partial writes are a correctness failure, not just a performance concern.

Typed setters (`UpdateEventTitle`, `UpdateEventStart`, etc.) were evaluated. At personal-calendar scale (100–500 changed events per sync), the additional DB round-trips are within the sub-5-second user experience budget. However, typed setters do not provide atomicity across multiple field updates without an explicit transaction wrapper, which would replicate what `EventPatch` provides for free.

## Considered Options

- **`map[string]any` with string whitelist:** flexible, but runtime key errors and no atomicity guarantee. Rejected.
- **Typed setters per field:** compile-time safe; multiple round-trips; no atomicity across fields without extra wrapper. Rejected in favour of EventPatch.
- **`EventPatch` struct with pointer fields (chosen):** compile-time safe, single atomic write, composable with conflict resolution (build one patch from the winning version and apply it).
