# Changelog

## v0.3.0 (unreleased)

- **GST-100–103, GST-110–114:** Save and resume runs and archive worlds with
  stable state and transition ordering, accepted patch IDs, and provenance.
- **GST-200–203:** Override randomness per step and attach application
  metadata to individual and joint records.
- **GST-300–303, GST-400–403:** Explain rejected candidates and permanently
  retire edges or states without erasing their history.
- **GST-500:** Inspect ordered nodes and edges with retirement and provenance.
- **GST-600–601, GST-700–703:** Add the acquaintance example, persistence
  and eligibility tests, fuzz targets, and benchmark verification.

### Migration from v0.2.0

Existing calls to `Step`, `StepTransition`, `ApplyAction`, encounter
`Step`, and `Together` keep their signatures and default behavior. Use
the corresponding `With` methods for caller-owned randomness and metadata.
Code using unkeyed literals of exported workflow structs should switch to
keyed literals. New JSON fields are optional in existing documents.

For a resumable world, save the original definition and accepted patches
rather than applying those patches to a definition that already includes
them. Save each run with `Snapshot` and pass the restored world to
`ResumeRun`. Keep application facts, timing and random seed derivation in
the consumer. See the README for concurrency and retirement behavior.
