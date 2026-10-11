# hue

A client for the local API of Philips Hue bridges in pure Go: CLIP v2, the event stream, pairing, a certificate check for bridge certificates, and API v1.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/hue.svg)](https://pkg.go.dev/github.com/cB-Sigma/hue) [![test](https://github.com/cB-Sigma/hue/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/hue/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.24-00ADD8.svg)](go.mod)

It reads, changes, creates and deletes CLIP v2 resources (the bridge's
API v2 over HTTPS), follows the event stream, pairs with the link
button, checks the bridge's certificate by its bridge ID and leaves the
trust decisions to the caller, and serves the round bridge and old
firmware through API v1 as CLIP v2-shaped resources, so one model covers
both. Two companion modules in this repository add discovery
(`hue/discover`: multicast DNS and the public endpoint) and
entertainment streaming (`hue/entertainment`: HueStream over DTLS with
pre-shared keys, pure Go). No cloud account, no cgo; the core module
needs only the standard library. It was written for SigmaM2M, a oneM2M
platform, where it drives the Hue integration.

Not affiliated with Philips or Signify (see NOTICE).

## Features

| Feature | Status |
|---|---|
| CLIP v2: GET, PUT, POST and DELETE of any resource type, error answers, partly applied changes (`Do`, `List`, `Get`, `Put`, `Post`, `Delete`) | yes |
| Rooms, zones, scenes and behaviours (behavior_instance): create, change, delete, scene recall (`CreateRoom`, `CreateZone`, `UpdateGroup`, `CreateScene`, `UpdateScene`, `RecallScene`, `CreateBehavior`, `UpdateBehavior`) | yes |
| Typed views of light, room and zone, grouped_light, scene, device, behavior_instance and entertainment_configuration (`As`, `ListAs`), next to the maps | yes |
| Event stream: SSE framing, add, update, delete; `Merge` applies partial resources | yes |
| Pairing: application key and client key; plain HTTP fallback for API v1 bridges; the application ID of a key (`ApplicationID`, GET /auth/v1) | yes |
| Certificate check: common name is the bridge ID, chain against given roots, pinning callback, accept-or-refuse callback; `RootsFromPEM` and `LoadRoots` for the Hue root CA | yes; the root CA itself is not shipped (see below) |
| Public configuration without a key; telling API v1 bridges (`NeedsV1`) | yes |
| API v1: lights, rooms, zones, light groups (LightGroup, Luminaire, LightSource as type `light_group`), entertainment groups (as `entertainment_configuration`, start and stop), group and light scenes, the home (group 0 as `bridge_home` with its grouped light), motion, temperature, light level, switches, battery, as CLIP v2-shaped resources; light, group, scene, room and zone changes; creating and deleting rooms, zones and scenes; button presses and motion between polls as events | yes; behaviours have no API v1 form and are refused |
| Discovery: multicast DNS `_hue._tcp` (bridge ID and model from TXT) and the public endpoint, merged (`hue/discover`) | yes, a module of its own (uses github.com/cB-Sigma/mdns) |
| Entertainment streaming: HueStream version 2 frames (RGB and XY), DTLS 1.2 with TLS_PSK_WITH_AES_128_GCM_SHA256, application ID as PSK identity, client key as key, start and stop through CLIP v2, resend of the last frame (`hue/entertainment`) | yes, a module of its own (uses pion/dtls); version 1 frames are read (`ParseV1`), not sent |
| SSDP discovery | no; bridges announce by mDNS, which `hue/discover` covers |
| The remote (cloud) API, firmware update | no (see Not yet) |

### Not yet

- The remote (cloud) API with OAuth: it needs a Hue developer account
  and an application registered with Signify to obtain client
  credentials; none was available for this work.
- A run against a real bridge, square and round, with the real Hue root
  CA chain: it needs the hardware. Every run so far was against diyHue
  and this repository's simulator.
- Firmware update requests: they cannot be tried without hardware.

### The Hue root CA

The Hue bridge root CA certificates are published by Signify on its
developer site, on a page behind a free developer login, with no
statement that permits redistribution. This module therefore does not
ship them (decision of 2026-10-11): fetch the PEM from the developer
site ("Using HTTPS" in the application design guidance), keep it with
your application, and load it with `hue.LoadRoots(path)` or
`hue.RootsFromPEM(data)` into `TLSCheck.Roots`. Some projects embed the
certificates in their source; whether that is permitted is between them
and Signify.

## Choosing a Go Hue library

Compared from each library's source at the version named, downloaded and
searched on 2026-10-11. Check the projects themselves before relying on
a row.

| | this module | openhue/openhue-go v0.4.0 | amimof/huego v1.2.1 | Collinux/gohue 1.0 |
|---|---|---|---|---|
| Latest release | v0.2.0 (2026-10-11) | 2025-04-16 | 2021-12-15 | 2018-12-29; archived |
| CLIP v2 resources | yes, any type, as JSON maps, with typed views of eight common types | yes, typed (generated from an OpenAPI description) | no | no |
| Creating and deleting | CLIP v2 rooms, zones, scenes, behaviours (and any type through `Post`/`Delete`); API v1 rooms, zones, scenes | generated client: POST and DELETE of the types its description lists | API v1: groups, scenes, rules, schedules, sensors, resource links | not checked |
| Event stream | yes | no | no | no |
| API v1 | yes, as CLIP v2-shaped resources | no | yes, typed | yes, typed |
| Pairing with a client key | yes | yes | yes | no client key |
| TLS to the bridge | checks the bridge ID as common name; roots, pin and accept callbacks | turns off verification on `http.DefaultTransport`, process-wide | plain HTTP | plain HTTP |
| Discovery | mDNS and the public endpoint (`hue/discover`) | mDNS and the public endpoint | the public endpoint | the public endpoint |
| Entertainment streaming | yes, DTLS in pure Go (`hue/entertainment`) | starts and stops a configuration; no stream | turns a group's stream flag on and off; no stream | no |
| `context.Context` | yes | generated client yes; the `Home` helpers no | yes (`…Context` variants) | no |
| Dependencies | core: standard library; `hue/discover`: cB-Sigma/mdns; `hue/entertainment`: pion/dtls | oapi-codegen runtime, grandcat/zeroconf and others | standard library | standard library |
| Licence | Apache-2.0 | Apache-2.0 | MIT | GPL-2.0 |

Pick this module for a long-running integration that has to stay in sync
through the event stream, keep certificate decisions under its own
control, serve round bridges, or stream to an entertainment area. Pick
openhue/openhue-go for typed models of every CLIP v2 type its
description covers, generated rather than written by hand, and
amimof/huego for an existing API v1 code base that uses rules and
schedules. This module's typed views cover the common types; other
types stay maps.

## Install

```
go get github.com/cB-Sigma/hue@latest
go get github.com/cB-Sigma/hue/entertainment@latest   # streaming
go get github.com/cB-Sigma/hue/discover@latest        # discovery
```

The core module and `hue/entertainment` require Go 1.24 or later;
`hue/discover` requires Go 1.27.1, as github.com/cB-Sigma/mdns does.

## Quick start

```go
import "github.com/cB-Sigma/hue"

check := hue.TLSCheck{BridgeID: func() string { return "001788fffe4a1b2c" }, Roots: hueRootCA}
c := hue.NewClient(hue.Config{Address: "192.168.1.20", TLS: check})
res, err := c.Pair(ctx, "myapp#kitchen") // hue.ErrLinkButton until the button is pressed

c = hue.NewClient(hue.Config{Address: "192.168.1.20", AppKey: res.AppKey, TLS: check})
lights, _ := c.List(ctx, "light")
_ = c.Put(ctx, "light", lights[0].ID(), map[string]any{"on": map[string]any{"on": false}})

body, _ := c.Events(ctx)
defer body.Close()
_ = hue.ReadEvents(body, func(evs []hue.Event) { /* merge e.Data into a cache with hue.Merge */ }, nil)
```

Creating and streaming:

```go
room, _ := c.CreateRoom(ctx, hue.GroupSpec{Name: "Study", Archetype: "office",
	Children: []hue.Ref{{RID: deviceID, RType: "device"}}})
scene, _ := c.CreateScene(ctx, hue.SceneSpec{Name: "Reading", Group: room,
	Actions: []hue.SceneAction{{Target: hue.Ref{RID: lightID, RType: "light"},
		Action: map[string]any{"on": map[string]any{"on": true}}}}})
_ = c.RecallScene(ctx, scene.RID, "")

areas, _ := hue.ListAs[hue.EntertainmentConfiguration](ctx, c, "entertainment_configuration")
s, _ := entertainment.Open(ctx, c, areas[0].ID, res.ClientKey, entertainment.Options{})
_ = s.Send(entertainment.RGBChannel(0, 1, 0.3, 0)) // many times a second
_ = s.Close(ctx)                                     // also stops the area

found, _ := discover.All(ctx, discover.Options{})
```

For a bridge that `NeedsV1` (from `ReadV1Config`), set
`c.V1 = hue.NewV1Client(address, key, nil)`: the same `List` and `Put`
calls are then served from API v1, and `c.V1.TakeEvents()` gives the
button presses and motion found between reads.

The certificate check leaves the decisions to the caller: `Pin` is told
each bridge's certificate fingerprint (refuse a changed one, or remember
it), and `Unverified` decides a certificate whose common name or chain
does not check out. The zero check refuses those.

`Example` in example_test.go shows pairing, reading and a change; it
compiles with the tests but does not run (it needs a bridge).
`ExampleReadEvents` and `ExampleLuxFromLightLevel` run and check their
output. hue-sim runs the same steps as `Example` against a simulated
bridge.

## Documentation

API reference: https://pkg.go.dev/github.com/cB-Sigma/hue

| Package | What it does |
|---|---|
| `hue` (module root) | `Client` (`Do`, `List`, `Get`, `Put`, `Post`, `Delete`, `Events`, `Pair`, `ApplicationID`, the create and update helpers, `StartEntertainment`/`StopEntertainment`), `Resource`/`Ref`/`Merge`, typed views (`Light`, `Group`, `Scene`, …, `As`, `ListAs`), `Event`/`ReadEvents`, `TLSCheck`, `RootsFromPEM`/`LoadRoots`, `ReadConfig`/`ReadV1Config`/`NeedsV1`, `V1Client`, `V1State`, `V1ButtonEvent`, `LuxFromLightLevel`, `NormaliseBridgeID` |
| `entertainment` (nested module github.com/cB-Sigma/hue/entertainment) | `Frame`, `Parse`, `ParseV1`, `RGBChannel`, `XYChannel`, `RGBToXY`, `Dial`/`Stream`, `Open`/`Session` |
| `discover` (nested module github.com/cB-Sigma/hue/discover) | `MDNS`, `Cloud`, `All`, `Merge`, `Announcement` |
| `interop` (nested module, tests only) | this client against diyHue in Docker |

## Testing

```
go test -race ./...
(cd entertainment && go test -race ./...)
(cd discover && go test -race ./...)
```

The unit tests run against a small fake bridge in fake_test.go;
`entertainment` tests its frames and its DTLS client against an
in-process pion/dtls PSK server; `discover` tests the endpoint against a
fake and multicast DNS over the loopback interface (skipped where
multicast does not come back to the host). The
client's tests against the full simulator are in
[hue-sim](https://github.com/cB-Sigma/hue-sim), so this module does not
depend on its simulator. One test carries a `// Covers: SIG-...` comment:
a requirement ID of the SigmaM2M project's compliance report, with no
meaning outside it.

`interop/` is a nested module with its own go.mod. Its tests start
diyHue's Docker image, pinned by digest, and are skipped unless
`SIGMA_HUE_INTEROP=1` is set and docker is on the path:

```
cd interop && SIGMA_HUE_INTEROP=1 go test -v ./...
```

### Interop tested against

- diyHue, an open-source Hue bridge emulator (its bridge emulator code
  is Apache-2.0; its light firmware directory is CC BY-NC 4.0 and is not
  used; written independently of this module; run only as its published
  image, nothing taken from it): image `diyhue/core` "latest" of
  2026-10-05, pinned as
  `sha256:5501ec39c4bf73e8f5b750988151b05e1fd4a0ef1602d8c98a9eaf80c0674e75`
  (source commit 3ee0461), Docker Desktop on macOS arm64, 2026-10-11.
  Passed: pairing refused before the link button and granted after it
  (application key and client key); the public configuration over HTTP
  and HTTPS, `NeedsV1` false for its API 6.1.0; the certificate check
  (common name equal to the bridge ID, chain verified against diyHue's
  self-signed certificate read out of the container, pin told the
  fingerprint, a wrong bridge ID refused, a chain problem handed to
  `Unverified`); CLIP v2 lists of bridge, device and light; a refused
  key; a light PUT (on and brightness) read back and reported on the
  event stream as a partial update that `Merge` applies; API v1 through
  `V1Client` (list and a light change) and a refused v1 key. diyHue's
  link button was pressed through its own configuration API from inside
  the container, and its light was one of its "dummy" lights (no
  hardware).
- The same image, 2026-10-11, for the v0.2.0 additions, all passed:
  creating a room (with the light's device), a zone, a scene (recalled)
  and a behaviour (with diyHue's first behavior_script), renaming the
  room, disabling the behaviour, deleting all four and seeing them gone
  from the lists; the typed views of a room and an entertainment
  configuration; `ApplicationID` (diyHue answers /auth/v1 with the
  application key itself); entertainment streaming: an
  entertainment_configuration created over the light's entertainment
  service, started with `entertainment.Open` (DTLS 1.2 PSK to diyHue's
  UDP port 2100, which diyHue serves by running `openssl s_server`,
  OpenSSL 3.5.7 in the image), XY frames sent at 50 a second, and the
  light's colour point (0.6, 0.35) and brightness (204 of 254) read back
  through API v1; stopping the configuration on close. diyHue lists a
  created room under a different ID than its POST answer returns; both
  IDs work for its GET, PUT and DELETE, and `Get` returns the one
  resource it answers. diyHue reads frames in fixed-size pieces, so a
  frame must carry every channel of the configuration.
- diyHue keeps each event for well under a second and its stream polls
  on a timer, so a just-opened stream sometimes misses a change; the
  test repeats the change up to five times, and in some runs the event
  arrived on the second.
- What that run does not show: diyHue's certificate is self-signed, not
  issued by the Hue root CA, so verification against the real root was
  not exercised; diyHue answers an unknown resource ID with an empty
  list rather than an error, so the 404 path was not checked there.
- The simulator in hue-sim, written by the same author from the same
  reading of the API, so not independent; the runs of aiohue,
  python-hue-v2 and openhue-go against that simulator are recorded in
  hue-sim.
- Not yet: a real Hue bridge (square or round), the Hue root CA chain,
  firmware-specific behaviour, entertainment streaming to a real bridge
  (diyHue's DTLS server is OpenSSL, a bridge's is its own), the remote
  API.

## Related repositories

- [hue-sim](https://github.com/cB-Sigma/hue-sim): simulated Hue bridges
  (CLIP v2 and API v1) as a Go package and a command.

## Status

v0.x: the API may change before v1.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam.
