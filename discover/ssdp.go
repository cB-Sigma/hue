// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package discover

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cB-Sigma/hue"
)

// SSDP (the search of UPnP) finds the round bridge (BSB001) and bridges
// with old firmware, which do not announce themselves by multicast DNS. A
// search is an M-SEARCH request sent to the SSDP multicast group; each
// bridge answers with the address of its description (LOCATION), a
// SERVER header naming IpBridge and, from firmware 1.9 on, its bridge ID
// (hue-bridgeid).

// SSDPAddr is the SSDP multicast group and port.
const SSDPAddr = "239.255.255.250:1900"

// SourceSSDP marks a bridge found by SSDP.
const SourceSSDP Source = "ssdp"

// DefaultSSDPWait bounds an SSDP search when neither the options nor the
// context set a limit.
const DefaultSSDPWait = 3 * time.Second

// SSDPOptions configure SSDP.
type SSDPOptions struct {
	// Addr is where the search is sent: SSDPAddr when empty; a unicast
	// address asks one device (tests use a responder on the loopback
	// interface).
	Addr string
	// Wait bounds the search; DefaultSSDPWait when zero and ctx has no
	// deadline.
	Wait time.Duration
	// MX is how many seconds answers may be spread over (1 to 5; 2 when
	// zero).
	MX int
}

// searchTargets are the search targets sent; bridges answer both.
var searchTargets = []string{"ssdp:all", "upnp:rootdevice"}

// SSDP searches for Hue bridges by SSDP and returns those that answered,
// one entry per bridge. Answers from other UPnP devices are left out.
func SSDP(ctx context.Context, o SSDPOptions) ([]Bridge, error) {
	to := o.Addr
	if to == "" {
		to = SSDPAddr
	}
	dst, err := net.ResolveUDPAddr("udp4", to)
	if err != nil {
		return nil, err
	}
	mx := o.MX
	if mx < 1 || mx > 5 {
		mx = 2
	}
	wait := o.Wait
	if wait <= 0 {
		wait = DefaultSSDPWait
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && (o.Wait <= 0 || d.Before(deadline)) {
		deadline = d
	}
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = c.SetReadDeadline(time.Now()) })
	defer stop()
	for _, st := range searchTargets {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: " + SSDPAddr + "\r\nMAN: \"ssdp:discover\"\r\nMX: " + strconv.Itoa(mx) + "\r\nST: " + st + "\r\n\r\n"
		if _, err := c.WriteToUDP([]byte(msg), dst); err != nil {
			return nil, err
		}
	}
	_ = c.SetReadDeadline(deadline)
	var found []Bridge
	buf := make([]byte, 4096)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break
			}
			if ctx.Err() != nil {
				break
			}
			return nil, err
		}
		if b, ok := ParseSSDP(buf[:n], from.IP); ok {
			found = append(found, b)
		}
	}
	return Merge(found), nil
}

// ParseSSDP reads one SSDP answer (or NOTIFY) from a Hue bridge; from is
// the sender's address, used when the answer names no host. ok is false
// for answers of other devices and for what is not SSDP.
func ParseSSDP(msg []byte, from net.IP) (Bridge, bool) {
	text := msg
	// http.ReadResponse wants a response line; a NOTIFY is a request.
	var h http.Header
	if bytes.HasPrefix(text, []byte("HTTP/")) {
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(text)), nil)
		if err != nil {
			return Bridge{}, false
		}
		_ = resp.Body.Close()
		h = resp.Header
	} else {
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(text)))
		if err != nil || req.Method != "NOTIFY" {
			return Bridge{}, false
		}
		h = req.Header
	}
	id := h.Get("hue-bridgeid")
	if !strings.Contains(h.Get("Server"), "IpBridge") && id == "" {
		return Bridge{}, false
	}
	b := Bridge{Port: 443, Location: h.Get("Location"), Sources: []Source{SourceSSDP}}
	if id != "" {
		b.ID = hue.NormaliseBridgeID(id)
	} else if usn := h.Get("Usn"); usn != "" {
		// Old firmware: the MAC address ends the UUID in USN; the bridge
		// ID is the MAC with fffe in its middle.
		u := strings.TrimPrefix(strings.ToLower(usn), "uuid:")
		u, _, _ = strings.Cut(u, "::")
		if i := strings.LastIndex(u, "-"); i >= 0 && len(u)-i-1 == 12 {
			mac := u[i+1:]
			if _, err := strconv.ParseUint(mac, 16, 64); err == nil {
				b.ID = mac[:6] + "fffe" + mac[6:]
			}
		}
	}
	if u, err := url.Parse(b.Location); err == nil && u.Hostname() != "" {
		b.Host = u.Hostname()
	} else if from != nil {
		b.Host = from.String()
	}
	if b.Host == "" {
		return Bridge{}, false
	}
	return b, true
}

// HTTPAddr is the address of the bridge's plain HTTP interface (API v1,
// the round bridge's only one): the host and port of its SSDP location
// when known, else the host on port 80.
func (b Bridge) HTTPAddr() string {
	if u, err := url.Parse(b.Location); err == nil && u.Hostname() != "" {
		port := u.Port()
		if port == "" {
			port = "80"
		}
		return net.JoinHostPort(u.Hostname(), port)
	}
	return net.JoinHostPort(b.Host, "80")
}
