# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

## v0.2.0 (2026-10-11)

### Added

- `Client.Get`, `Post`, `Delete`; `CreateRoom`, `CreateZone`,
  `UpdateGroup` (`GroupSpec`), `CreateScene`, `UpdateScene`,
  `RecallScene` (`SceneSpec`, `SceneAction`), `CreateBehavior`,
  `UpdateBehavior` (`BehaviorSpec`), `RefBody`, `ErrNoReference`,
  `Resource.Ref`.
- `Client.ApplicationID` (GET /auth/v1), `StartEntertainment`,
  `StopEntertainment`.
- Typed views: `Light`, `Group`, `GroupedLight`, `Scene`, `Device`,
  `BehaviorInstance`, `EntertainmentConfiguration` and their parts, with
  `As` and `ListAs`.
- `RootsFromPEM` and `LoadRoots` for the Hue root CA the caller supplies.
- `V1Client`: light groups (LightGroup, Luminaire, LightSource) as
  `light_group`, entertainment groups as `entertainment_configuration`
  (start and stop as the group's stream), light scenes (recalled through
  group 0), group 0 as `bridge_home` with its grouped light; GET of one
  resource; POST of rooms, zones and scenes (with light states), PUT of
  rooms, zones and scene names and light states, DELETE of groups and
  scenes.
- New nested module github.com/cB-Sigma/hue/entertainment: HueStream
  frames (`Frame`, `Parse`, `ParseV1`, channels in RGB and XY,
  `RGBToXY`), the DTLS 1.2 PSK client on pion/dtls (`Dial`, `Stream`,
  with resend of the last frame) and `Open`/`Session`, which start and
  stop the configuration through CLIP v2.
- New nested module github.com/cB-Sigma/hue/discover: multicast DNS
  (`MDNS`), the public endpoint with an injectable client and URL
  (`Cloud`), both merged (`All`, `Merge`), and the announcement a bridge
  makes (`Announcement`).
- interop/: creation and deletion, and entertainment streaming, against
  diyHue.

### Changed

- `V1Client` room archetypes are written as CLIP v2 spells them
  ("living_room", not "living room").
- `V1Client` lists the scenes without a room that it skipped before, so
  scene lists of API v1 bridges can be longer.

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
