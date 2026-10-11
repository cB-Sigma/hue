// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package entertainment_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"github.com/cB-Sigma/hue/entertainment"
	"github.com/pion/dtls/v3"
)

const cfgID = "1a8d99cc-967b-44f2-9202-43f976c0fa6b"

func TestFrameRoundTrip(t *testing.T) {
	f := entertainment.Frame{ConfigID: cfgID, Space: entertainment.XY, Seq: 7,
		Channels: []entertainment.Channel{entertainment.XYChannel(0, 0.3127, 0.329, 1), entertainment.XYChannel(3, 0.7, 0.3, 0.5)}}
	b, err := f.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != entertainment.HeaderLen+14 || string(b[:9]) != "HueStream" || b[9] != 2 || b[10] != 0 || b[11] != 7 || b[14] != 1 || string(b[16:52]) != cfgID {
		t.Fatalf("header % x", b[:52])
	}
	// Channel 3: x 0.7 → 45875 (0xb333), big-endian after the ID.
	if !bytes.Equal(b[59:66], []byte{3, 0xb3, 0x33, 0x4c, 0xcd, 0x80, 0x00}) {
		t.Errorf("channel 3 % x", b[59:66])
	}
	g, err := entertainment.Parse(b)
	if err != nil || g.ConfigID != cfgID || g.Space != entertainment.XY || g.Seq != 7 || len(g.Channels) != 2 || g.Channels[1] != f.Channels[1] {
		t.Fatalf("parsed %+v %v", g, err)
	}
	if x, y, bri := g.Channels[0].Values(); math.Abs(x-0.3127) > 1e-4 || math.Abs(y-0.329) > 1e-4 || bri != 1 {
		t.Errorf("values %v %v %v", x, y, bri)
	}
	// Refusals.
	for name, bad := range map[string][]byte{
		"short":        b[:20],
		"magic":        append([]byte("HueStreem"), b[9:]...),
		"odd channels": b[:len(b)-1],
		"version 1":    append(append([]byte(nil), b[:9]...), append([]byte{1, 0}, b[11:]...)...),
		"colour space": append(append([]byte(nil), b[:14]...), append([]byte{9}, b[15:]...)...),
	} {
		if _, err := entertainment.Parse(bad); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := (entertainment.Frame{ConfigID: "short"}).MarshalBinary(); err == nil {
		t.Error("short ID marshalled")
	}
	if _, err := (entertainment.Frame{ConfigID: cfgID, Channels: make([]entertainment.Channel, 21)}).MarshalBinary(); err == nil {
		t.Error("21 channels marshalled")
	}
	if entertainment.RGB.String() != "RGB" || entertainment.XY.String() != "XY" {
		t.Error("names")
	}
}

func TestParseV1(t *testing.T) {
	b := append([]byte("HueStream"), 1, 0, 0, 0, 0, 0, 0, 0, 0x00, 0x03, 0xff, 0xff, 0x00, 0x00, 0x80, 0x00)
	sp, lights, err := entertainment.ParseV1(b)
	if err != nil || sp != entertainment.RGB || len(lights) != 1 || lights[0].ID != 3 || lights[0].A != 0xffff || lights[0].C != 0x8000 {
		t.Fatalf("v1 %v %+v %v", sp, lights, err)
	}
	if _, _, err := entertainment.ParseV1(b[:20]); err == nil {
		t.Error("cut v1 frame parsed")
	}
	if _, err := entertainment.Parse(b); !errors.Is(err, entertainment.ErrVersion) {
		t.Errorf("v1 frame as v2: %v", err)
	}
}

func TestRGBToXY(t *testing.T) {
	cases := []struct{ r, g, b, x, y float64 }{
		{1, 0, 0, 0.64, 0.33}, {0, 1, 0, 0.30, 0.60}, {0, 0, 1, 0.15, 0.06}, {1, 1, 1, 0.3127, 0.3290},
	}
	for _, c := range cases {
		x, y, _ := entertainment.RGBToXY(c.r, c.g, c.b)
		if math.Abs(x-c.x) > 0.002 || math.Abs(y-c.y) > 0.002 {
			t.Errorf("%v %v %v: %.4f %.4f", c.r, c.g, c.b, x, y)
		}
	}
	if _, _, bri := entertainment.RGBToXY(0, 0, 0); bri != 0 {
		t.Errorf("black %v", bri)
	}
	if _, _, bri := entertainment.RGBToXY(1, 1, 1); math.Abs(bri-1) > 1e-3 {
		t.Errorf("white %v", bri)
	}
}

func TestParseClientKey(t *testing.T) {
	if k, err := entertainment.ParseClientKey("00112233445566778899AABBCCDDEEFF"); err != nil || len(k) != 16 || k[15] != 0xff {
		t.Errorf("%x %v", k, err)
	}
	for _, bad := range []string{"", "0011", "zz112233445566778899AABBCCDDEEFF"} {
		if _, err := entertainment.ParseClientKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// pskServer is a DTLS PSK server like a bridge's: one key per identity;
// it hands each decoded frame to got.
func pskServer(t *testing.T, keys map[string][]byte, got chan<- entertainment.Frame) string {
	t.Helper()
	ln, err := dtls.Listen("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, &dtls.Config{
		PSK: func(id []byte) ([]byte, error) {
			k, ok := keys[string(id)]
			if !ok {
				return nil, errors.New("unknown identity")
			}
			return k, nil
		},
		PSKIdentityHint: []byte("bridge"),
		CipherSuites:    []dtls.CipherSuiteID{dtls.TLS_PSK_WITH_AES_128_GCM_SHA256},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 2048)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if f, err := entertainment.Parse(buf[:n]); err == nil {
						got <- f
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestStream(t *testing.T) {
	key := []byte("0123456789abcdef")
	got := make(chan entertainment.Frame, 256)
	addr := pskServer(t, map[string][]byte{"app-1": key}, got)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := entertainment.Dial(ctx, entertainment.Config{Addr: addr, Identity: "app-1", PSK: key, ConfigID: cfgID, Space: entertainment.RGB, Repeat: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(entertainment.RGBChannel(0, 1, 0, 0), entertainment.RGBChannel(1, 0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	f := <-got
	if f.ConfigID != cfgID || f.Space != entertainment.RGB || len(f.Channels) != 2 || f.Channels[0].A != 65535 || f.Channels[1].C != 65535 || f.Seq != 0 {
		t.Fatalf("frame %+v", f)
	}
	// Repeat resends the last frame with the next sequence numbers.
	select {
	case f := <-got:
		if f.Seq == 0 || f.Channels[0].A != 65535 {
			t.Errorf("repeated %+v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no repeated frame")
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
	if err := s.Send(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("send after close: %v", err)
	}

	// A wrong key fails the handshake.
	short, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if _, err := entertainment.Dial(short, entertainment.Config{Addr: addr, Identity: "app-1", PSK: []byte("fedcba9876543210"), ConfigID: cfgID}); err == nil {
		t.Error("wrong key: no error")
	}
	if _, err := entertainment.Dial(short, entertainment.Config{Addr: addr, Identity: "app-1", PSK: key, ConfigID: "x"}); err == nil {
		t.Error("bad configuration ID: no error")
	}
	if _, err := entertainment.Dial(short, entertainment.Config{Addr: addr, ConfigID: cfgID}); err == nil {
		t.Error("no identity: no error")
	}
}
