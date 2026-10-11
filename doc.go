// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package hue is a client for the local API of Philips Hue bridges, in
// pure Go: CLIP v2 (the bridge's API v2 over HTTPS) with its resources,
// changes and event stream, the link-button pairing that issues an
// application key, the bridge certificate check, and API v1 for the round
// bridge and firmware without CLIP v2.
//
//   - Client: GET, PUT, POST and DELETE of CLIP v2 resources with the
//     hue-application-key header and the bridge's error answers (Do, List,
//     Get, Put, Post, Delete), the event stream (Events, read with
//     ReadEvents), pairing (Pair, ErrLinkButton).
//   - Creating, changing and deleting rooms, zones, scenes and behaviours
//     (CreateRoom, CreateZone, UpdateGroup, CreateScene, UpdateScene,
//     RecallScene, CreateBehavior, UpdateBehavior, Delete); the
//     application ID of a key and starting or stopping an entertainment
//     configuration (ApplicationID, StartEntertainment,
//     StopEntertainment).
//   - Light, Group, Scene, GroupedLight, Device, BehaviorInstance,
//     EntertainmentConfiguration: optional typed views of resources (As,
//     ListAs).
//   - Resource, Ref, Merge: resources as the bridge sends them, and
//     applying the partial resources of update events.
//   - TLSCheck: the bridge's certificate carries its bridge ID as common
//     name; the check tests that, verifies the chain when roots are given,
//     and hands pinning and the accept-or-refuse decision to callbacks;
//     RootsFromPEM and LoadRoots load the Hue root CA the caller supplies.
//   - ReadConfig, ReadV1Config, NeedsV1: the public configuration (no key)
//     and whether a bridge speaks only API v1.
//   - V1Client: API v1, turned into CLIP v2-shaped resources so one model
//     serves both (rooms, zones, light groups, entertainment groups, group
//     and light scenes); button presses and motion are told from changes
//     between polls.
//
// Discovery (multicast DNS _hue._tcp and the public discovery endpoint)
// is in the module github.com/cB-Sigma/hue/discover, entertainment
// streaming (DTLS) in github.com/cB-Sigma/hue/entertainment; both are
// modules of their own so this one needs only the standard library.
// Simulated bridges for tests, and a command that runs one, are in the
// module github.com/cB-Sigma/hue-sim.
package hue
