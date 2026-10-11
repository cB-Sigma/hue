// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package interop holds tests only: the hue client against diyHue, an
// open-source Hue bridge emulator (Apache-2.0) written independently of
// this module, running in its published Docker image pinned by digest.
//
// The tests start the container with its HTTP and HTTPS ports published on
// 127.0.0.1, open diyHue's link-button window from inside the container
// (diyHue accepts configuration changes from its own loopback address
// without a key), pair, add a light of diyHue's "dummy" protocol (no
// hardware), and then exercise the public configuration, the certificate
// check, CLIP v2 reads and changes, the event stream, API v1 through
// V1Client, creating and deleting rooms, zones, scenes and behaviours,
// and entertainment streaming (the container's UDP port 2100 is
// published too; diyHue answers the DTLS stream with an openssl s_server
// it starts per stream). No diyHue code is kept in this repository. The
// tests run only
// when SIGMA_HUE_INTEROP=1 and docker is on PATH:
//
//	cd interop && SIGMA_HUE_INTEROP=1 GOWORK=off go test -v -timeout 10m ./...
//
// diyHue's certificate is self-signed with the bridge ID as common name;
// it is not issued by the Hue root CA. The tests therefore check the
// common name, pin the fingerprint, and verify the chain against the
// certificate read out of the container, never against the Hue root CA.
package interop
