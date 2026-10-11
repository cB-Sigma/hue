# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

## v0.1.0 (2026-10-11)

First release as a repository of its own, split from SigmaM2M
(2026-10-11): `Client` (`Do`, `List`, `Put`, `Events`, `Pair`),
`Resource`, `Ref`, `Merge`, `Event`, `ReadEvents`, `TLSCheck`,
`ReadConfig`, `ReadV1Config`, `NeedsV1`, `V1Client`, `V1State`,
`V1ButtonEvent`, `LuxFromLightLevel` and `NormaliseBridgeID`.

### Changed

- The simulator and its command moved to their own module,
  github.com/cB-Sigma/hue-sim. The library's unit tests run against a
  small fake bridge; `Example` compiles but no longer runs (hue-sim runs
  the same steps against a simulated bridge).

### Fixed

- `V1Client` light services carry `metadata` (name and archetype), as
  CLIP v2 light resources do. Found against diyHue's API v1.

### Added

- interop/: a test-only module that runs the client against diyHue in
  Docker.
