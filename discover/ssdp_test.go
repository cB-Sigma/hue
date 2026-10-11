// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package discover_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cB-Sigma/hue/discover"
)

// responder answers M-SEARCH on a loopback UDP port with the given
// answers (made-up demo values) and counts the searches.
func responder(t *testing.T, answers ...string) (string, func() []string) {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var mu sync.Mutex
	var got []string
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			got = append(got, string(buf[:n]))
			mu.Unlock()
			for _, a := range answers {
				_, _ = c.WriteToUDP([]byte(a), from)
			}
		}
	}()
	return c.LocalAddr().String(), func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }
}

const (
	roundAnswer = "HTTP/1.1 200 OK\r\nHOST: 239.255.255.250:1900\r\nEXT:\r\nCACHE-CONTROL: max-age=100\r\n" +
		"LOCATION: http://192.168.1.40:80/description.xml\r\nSERVER: Linux/3.14.0 UPnP/1.0 IpBridge/1.16.0\r\n" +
		"hue-bridgeid: 001788FFFE1A2B3C\r\nST: upnp:rootdevice\r\nUSN: uuid:2f402f80-da50-11e1-9b23-0017881a2b3c::upnp:rootdevice\r\n\r\n"
	// Firmware before the hue-bridgeid header: the ID comes from USN.
	oldAnswer = "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.1.41:80/description.xml\r\nSERVER: FreeRTOS/6.0.5, UPnP/1.0, IpBridge/0.1\r\n" +
		"ST: upnp:rootdevice\r\nUSN: uuid:2f402f80-da50-11e1-9b23-001788102201::upnp:rootdevice\r\n\r\n"
	// Another UPnP device: left out.
	tvAnswer = "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.1.50:1400/xml/device_description.xml\r\nSERVER: Linux UPnP/1.0 Sonos/70.3\r\n" +
		"ST: upnp:rootdevice\r\nUSN: uuid:RINCON_000E58000000::upnp:rootdevice\r\n\r\n"
)

func TestSSDP(t *testing.T) {
	addr, searches := responder(t, roundAnswer, oldAnswer, tvAnswer, "not ssdp at all")
	got, err := discover.SSDP(context.Background(), discover.SSDPOptions{Addr: addr, Wait: 500 * time.Millisecond, MX: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("found %+v", got)
	}
	// Sorted by ID; each answered twice (two search targets), merged.
	old, round := got[0], got[1]
	if round.ID != "001788fffe1a2b3c" || round.Host != "192.168.1.40" || round.Location != "http://192.168.1.40:80/description.xml" ||
		round.HTTPAddr() != "192.168.1.40:80" || round.Port != 443 || len(round.Sources) != 1 || round.Sources[0] != discover.SourceSSDP {
		t.Errorf("round bridge %+v", round)
	}
	if old.ID != "001788fffe102201" || old.Host != "192.168.1.41" {
		t.Errorf("old firmware %+v", old)
	}
	s := searches()
	if len(s) != 2 || !strings.Contains(s[0], "ST: ssdp:all") || !strings.Contains(s[1], "ST: upnp:rootdevice") ||
		!strings.Contains(s[0], `MAN: "ssdp:discover"`) || !strings.Contains(s[0], "MX: 1") || !strings.HasPrefix(s[0], "M-SEARCH * HTTP/1.1\r\n") {
		t.Errorf("searches %q", s)
	}
	// The context ends the search early.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := discover.SSDP(ctx, discover.SSDPOptions{Addr: addr, Wait: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the search outlived its context: %v", time.Since(start))
	}
}

func TestParseSSDPNotify(t *testing.T) {
	notify := "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nCACHE-CONTROL: max-age=100\r\nLOCATION: http://192.168.1.42:80/description.xml\r\n" +
		"SERVER: Linux/3.14.0 UPnP/1.0 IpBridge/1.16.0\r\nNTS: ssdp:alive\r\nhue-bridgeid: 001788FFFE4A1B2C\r\nNT: upnp:rootdevice\r\n\r\n"
	b, ok := discover.ParseSSDP([]byte(notify), net.IPv4(192, 168, 1, 42))
	if !ok || b.ID != "001788fffe4a1b2c" || b.Host != "192.168.1.42" {
		t.Errorf("notify %+v %v", b, ok)
	}
	// Without a location the sender's address is the host.
	b, ok = discover.ParseSSDP([]byte("HTTP/1.1 200 OK\r\nSERVER: x IpBridge/1.0\r\nhue-bridgeid: 001788fffe4a1b2c\r\n\r\n"), net.IPv4(10, 0, 0, 9))
	if !ok || b.Host != "10.0.0.9" || b.HTTPAddr() != "10.0.0.9:80" {
		t.Errorf("no location %+v %v", b, ok)
	}
	if _, ok := discover.ParseSSDP([]byte("M-SEARCH * HTTP/1.1\r\n\r\n"), nil); ok {
		t.Error("a search taken for an answer")
	}
}

func TestAllWithSSDP(t *testing.T) {
	addr, _ := responder(t, roundAnswer)
	url := cloud(t, 200, `[{"id":"001788fffe1a2b3c","internalipaddress":"192.168.1.40","port":443}]`)
	got, err := discover.All(context.Background(), discover.Options{NoMDNS: true, CloudURL: url, SSDP: true,
		SSDPOptions: discover.SSDPOptions{Addr: addr, Wait: 300 * time.Millisecond}})
	if err != nil || len(got) != 1 || len(got[0].Sources) != 2 || got[0].Location == "" {
		t.Fatalf("all %+v %v", got, err)
	}
	// SSDP alone counts as a search.
	got, err = discover.All(context.Background(), discover.Options{NoMDNS: true, NoCloud: true, SSDP: true,
		SSDPOptions: discover.SSDPOptions{Addr: addr, Wait: 300 * time.Millisecond}})
	if err != nil || len(got) != 1 {
		t.Errorf("ssdp only %+v %v", got, err)
	}
}
