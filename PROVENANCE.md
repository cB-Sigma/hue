# Provenance

## How the code was written

Written in Go for SigmaM2M, a oneM2M platform. No code was ported or
copied from another implementation, and no source file of another
project was translated.

The CLIP v2 client, the event stream reader, pairing and the certificate
check came with SigmaM2M's first version (2026-09-27). The API v1 client
(`V1Client`, `V1State`, `ReadV1Config`, `NeedsV1`, the plain HTTP
fallback of pairing) was added on 2026-10-05. A review of the same day
checked the light body shapes and the error handling against the CLIP v2
documentation. The files marked "Copyright 2026 Abhinav Gautam" were
written for this module from SigmaM2M's integration code (the resource
helpers, the event stream reader and the TLS check).

The module became a library of its own on 2026-10-06, and on 2026-10-11
its simulator moved to the separate module github.com/cB-Sigma/hue-sim;
the library's own tests now run against a small fake bridge
(fake_test.go) written for that purpose.

Before the split, existing Go libraries were checked and not used:
openhue/openhue-go (Apache-2.0; a generated CLIP v2 client without the
event stream or API v1), amimof/huego (MIT; API v1 only) and
Collinux/gohue (GPL-2.0; API v1 only, archived). No code was taken from
them. The comparison table in the README was made from their source at
the versions it names.

## Sources

| Area (files) | Followed | Read for facts only (no code taken) |
|---|---|---|
| CLIP v2 requests, resources, error answers (client.go, resource.go) | Philips Hue developer documentation, API v2 ("Getting started" and the resource pages), as far as public | public descriptions of CLIP v2 |
| Event stream (events.go) | the same documentation; the server-sent events format of the WHATWG HTML standard | none |
| Pairing (client.go: Pair) | the same documentation: POST /api with devicetype and generateclientkey; error type 101 | none |
| Certificate check (tls.go) | Philips Hue developer documentation, "Using HTTPS": the bridge ID as common name, the Hue root CA | none |
| Light level (resource.go: LuxFromLightLevel) | the documented light_level scale, 10000·log10(lux)+1 | none |
| API v1 (v1.go) | Philips Hue API v1 documentation (lights, groups, sensors, scenes, configuration, error types, buttonevent codes) | none |

Parts of the API v2 documentation need a developer account and were not
read for this work. The facts the client relies on that only those pages
would confirm are: the full set of error descriptions, the absence of a
keep-alive on the event stream (the client sets TCP keep-alive instead),
and the exact light_level scale. They should be checked against those
pages when an account is available.

## Passages closest to a public source

No passage is known to be copied. These are the places closest to a
public source, because they are tables of facts that every client holds
in the same form:

- `v1Archetypes` in v1.go: v1 archetype names and their v2 spellings.
- `V1ButtonEvent` in v1.go: the buttonevent codes, including the fixed
  codes of the tap switch.
- `NeedsV1` in v1.go: the API version (1.46) from which bridges have
  CLIP v2.

## Independent verification

The interop module (interop/) runs this client against diyHue
(Apache-2.0), an open-source bridge emulator written independently of
this module, in its published Docker image pinned by digest
(sha256:5501ec39c4bf73e8f5b750988151b05e1fd4a0ef1602d8c98a9eaf80c0674e75,
the "latest" image of 2026-10-05, source commit 3ee0461). On 2026-10-11
pairing, the public configuration, the certificate check against
diyHue's self-signed certificate, CLIP v2 reads, a light change, the
event stream and API v1 passed; the README lists what was exercised and
what was not. diyHue's source at that commit was read only to find how
to open its link-button window and add a light without hardware; nothing
was taken from it.

That run found one bug, fixed with a regression test (TestClientV1 in
client_test.go): `V1Client` gave light services no `metadata`, so the
light's name and archetype, which CLIP v2 puts there, were missing for
API v1 bridges.

## Test data

All bridge IDs, keys, names, MAC addresses, firmware versions and
addresses in the tests and examples are made-up demo values. The MAC
address given to diyHue (02:42:ac:11:00:aa) is a locally administered
demo value.
