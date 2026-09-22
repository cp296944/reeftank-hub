# K7 legacy synchronization

K7 functionality is developed in ReefTank Hub and synchronized one way into
the legacy K7 repository through a pull request.

Safety properties:

- Only paths in `k7-paths.txt` are copied.
- Hub-only packages such as bootstrap, dosing, HA, storage and equipment are
  excluded.
- Import paths and the legacy platform identifier are transformed back for the
  old Go module.
- Legacy K7 tests run before a branch is pushed.
- The workflow opens a PR against `dev/pi-bridge`; it never pushes directly to
  that branch or to `master`.
- If `K7_SYNC_TOKEN` is absent, the workflow performs no external write.

`K7_SYNC_TOKEN` should be a fine-grained token limited to
`cp296944/k7-led-Raspberry-controller`, with only Contents read/write and Pull
requests read/write. Store it as a repository Actions secret; never commit it.
