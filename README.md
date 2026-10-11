# hue

A client for the local API of Philips Hue bridges in pure Go: CLIP v2, the event stream, pairing, a certificate check for bridge certificates, and API v1.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/hue.svg)](https://pkg.go.dev/github.com/cB-Sigma/hue) [![test](https://github.com/cB-Sigma/hue/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/hue/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.24-00ADD8.svg)](go.mod)

It reads and changes any CLIP v2 resource (the bridge's API v2 over
HTTPS), follows the event stream, pairs with the link button, checks the
bridge's certificate by its bridge ID and leaves the trust decisions to
the caller, and serves the round bridge and old firmware through API v1
as CLIP v2-shaped resources, so one model covers both. No cloud account,
no cgo, no dependencies beyond the standard library. It was written for
SigmaM2M, a oneM2M platform, where it drives the Hue integration.

Not affiliated with Philips or Signify (see NOTICE).

## Features

| Feature | Status |
|---|---|
| CLIP v2: GET and PUT of any resource type, error answers, partly applied changes | yes; other methods through `Do` |
| Event stream: SSE framing, add, update, delete; `Merge` applies partial resources | yes |
| Pairing: application key and client key; plain HTTP fallback for API v1 bridges | yes |
| Certificate check: common name is the bridge ID, chain against given roots, pinning callback, accept-or-refuse callback | yes; the Hue root CA is not shipped |
| Public configuration without a key; telling API v1 bridges (`NeedsV1`) | yes |
| API v1: lights, rooms and zones, group scenes, motion, temperature, light level, switches, battery, as CLIP v2-shaped resources; light, group and scene changes; button presses and motion between polls as events | yes; LightGroup, Luminaire and Entertainment groups and scenes without a room are skipped |
| Discovery (mDNS `_hue._tcp`, SSDP, the public endpoint) | no; use an mDNS or SSDP library |
| Entertainment streaming (DTLS), the remote (cloud) API, firmware update, creating rooms, scenes or automations | no |

## Choosing a Go Hue library

Compared from each library's source at the version named, downloaded and
searched on 2026-10-11. Check the projects themselves before relying on
a row.

| | this module | openhue/openhue-go v0.4.0 | amimof/huego v1.2.1 | Collinux/gohue 1.0 |
|---|---|---|---|---|
| Latest release | v0.1.0 (2026-10-11) | 2025-04-16 | 2021-12-15 | 2018-12-29; archived |
| CLIP v2 resources | yes, any type, as JSON maps | yes, typed (generated from an OpenAPI description) | no | no |
| Event stream | yes | no | no | no |
| API v1 | yes, as CLIP v2-shaped resources | no | yes, typed | yes, typed |
| Pairing with a client key | yes | yes | yes | no client key |
| TLS to the bridge | checks the bridge ID as common name; roots, pin and accept callbacks | turns off verification on `http.DefaultTransport`, process-wide | plain HTTP | plain HTTP |
| Discovery | no | mDNS and the public endpoint | the public endpoint | the public endpoint |
| `context.Context` | yes | generated client yes; the `Home` helpers no | yes (`…Context` variants) | no |
| Dependencies | standard library | oapi-codegen runtime, grandcat/zeroconf and others | standard library | standard library |
| Licence | Apache-2.0 | Apache-2.0 | MIT | GPL-2.0 |

Pick this module for a long-running integration that has to stay in sync
through the event stream, keep certificate decisions under its own
control, and also serve round bridges. Pick openhue/openhue-go for typed
CLIP v2 models and built-in discovery in a short-lived tool, and
amimof/huego for an existing API v1 code base. This module returns
resources as maps rather than typed structs, which keeps it small and
lets new resource types through, at the cost of compile-time field
checks.

## Install

```
go get github.com/cB-Sigma/hue@latest
```

Requires Go 1.24 or later.

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
| `hue` (module root) | `Client` (`Do`, `List`, `Put`, `Events`, `Pair`), `Resource`/`Ref`/`Merge`, `Event`/`ReadEvents`, `TLSCheck`, `ReadConfig`/`ReadV1Config`/`NeedsV1`, `V1Client`, `V1State`, `V1ButtonEvent`, `LuxFromLightLevel`, `NormaliseBridgeID` |
| `interop` (nested module, tests only) | this client against diyHue in Docker |

## Testing

```
go test -race ./...
```

The unit tests run against a small fake bridge in fake_test.go. The
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

- diyHue, an open-source Hue bridge emulator (Apache-2.0, written
  independently of this module): image `diyhue/core` "latest" of
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
- diyHue keeps each event for well under a second and its stream polls
  on a timer, so a just-opened stream sometimes misses a change; the
  test repeats the change up to five times, and in some runs the event
  arrived on the second.
- What that run does not show: diyHue's certificate is self-signed, not
  issued by the Hue root CA, so verification against the real root was
  not exercised; diyHue answers an unknown resource ID with an empty
  list rather than an error, so the 404 path was not checked there.
- The simulator in hue-sim, written by the same author from the same
  reading of the API, so not independent; aiohue's run against that
  simulator is recorded in hue-sim.
- Not yet: a real Hue bridge (square or round), the Hue root CA chain,
  firmware-specific behaviour.

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
