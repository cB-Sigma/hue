# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

## v0.3.0 (2026-10-11)

### Added

- API v1 beyond the CLIP-shaped resources, on `V1Client` (v1api.go):
  rules (`Rules`, `Rule`, `CreateRule`, `UpdateRule`, `DeleteRule`,
  `V1Rule`, `V1Condition`, `V1Action`), schedules (`Schedules`,
  `Schedule`, `CreateSchedule`, `UpdateSchedule`, `DeleteSchedule`,
  `V1Schedule`), resource links (`ResourceLinks`, `ResourceLink`,
  `CreateResourceLink`, `UpdateResourceLink`, `DeleteResourceLink`,
  `V1ResourceLink`), sensors (`Sensors`, `Sensor`, `CreateSensor` for
  CLIP sensors, `RenameSensor`, `SetSensorState`, `SetSensorConfig`,
  `DeleteSensor`, `V1Sensor`), the search (`SearchLights`, `NewLights`,
  `SearchSensors`, `NewSensors`, `V1NewDevices`), lights (`RenameLight`,
  `IdentifyLight`, `DeleteLight`), the configuration (`Config`,
  `V1BridgeConfig`, `UpdateConfig`, `DeleteUser`, `V1WhitelistEntry`),
  `Capabilities` (`V1Capabilities`, `V1Capacity`), `FullState`, and the
  software update flags (`SoftwareUpdate`, `CheckForUpdate`,
  `InstallUpdate`, `SetAutoInstall`, `V1SoftwareUpdate`; tested against
  the simulator only).
- `Client.APIv1`: API v1 over the client's HTTPS connection and
  certificate check, for square bridges.
- `V1Address`: the v1 address of a CLIP-shaped ID made by `V1Client`.
- `APIError.Codes` (the API v1 error types) and `APIError.HasCode`.
- Typed views (models_services.go): `Motion`, `CameraMotion`,
  `GroupedMotion`, `Temperature`, `LightLevel`, `GroupedLightLevel`,
  `Button`, `RelativeRotary`, `Contact`, `Tamper`, `DevicePower`,
  `ZigbeeConnectivity`, `ZGPConnectivity`, `EntertainmentService`,
  `Bridge`, `BridgeHome`, `SmartScene`, `BehaviorScript`,
  `DeviceSoftwareUpdate`, `Geolocation`, `Homekit`, `Matter`,
  `GeofenceClient` and their parts, with `Value`, `Event` and `Open`
  helpers; `Light` gained `Alert`, `Effects`, `Gradient`, `Powerup`,
  `TimedEffects` and `Signaling`.
- hue/discover: SSDP (`SSDP`, `SSDPOptions`, `SSDPAddr`, `ParseSSDP`,
  `SourceSSDP`, `DefaultSSDPWait`), `Bridge.Location`,
  `Bridge.HTTPAddr`, and `Options.SSDP`/`Options.SSDPOptions` to add it
  to `All` (off by default, so `All` searches as before).
- interop/: rules, schedules, resource links, sensors, configuration,
  whitelist, capabilities, search, API v1 over HTTPS and SSDP against
  diyHue. The interop module now needs Go 1.27.1 (it uses hue/discover).

### Changed

- `APIError` has a new field (`Codes`); composite literals of it without
  field names no longer compile.
- `V1Client` reads the bridge again after any change made through it
  (it already did after CLIP-shaped changes).

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
