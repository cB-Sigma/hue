// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cB-Sigma/hue"
)

// The client's own tests run against fakeBridge (fake_test.go). The same
// behaviours, and more, are tested against the full simulator in
// github.com/cB-Sigma/hue-sim.

func fakeCheck(t *testing.T, f *fakeBridge) hue.TLSCheck {
	return hue.TLSCheck{BridgeID: func() string { return f.id }, Roots: f.roots(t)}
}

func TestClientListPutErrors(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	c := hue.NewClient(hue.Config{Address: f.addr(), AppKey: fakeKey, TLS: fakeCheck(t, f), Timeout: 5 * time.Second})
	defer c.CloseIdleConnections()
	ctx := context.Background()
	if c.BaseURL() != "https://"+f.addr() || c.HTTPClient() == nil {
		t.Errorf("base %s", c.BaseURL())
	}
	lights, err := c.List(ctx, "light")
	if err != nil || len(lights) != 2 || lights[0].Owner() != "dev-1" || lights[0].Str("metadata", "name") != "Sofa lamp" {
		t.Fatalf("lights %v %v", lights, err)
	}
	if err := c.Put(ctx, "light", "light-1", map[string]any{"on": map[string]any{"on": false}}); err != nil {
		t.Fatal(err)
	}
	if lights, _ := c.List(ctx, "light"); lights[0].Get("on", "on") != false {
		t.Errorf("after PUT %v", lights[0])
	}
	var ae *hue.APIError
	err = c.Put(ctx, "light", "light-2", map[string]any{"color": map[string]any{"xy": map[string]any{"x": 0.3, "y": 0.3}}})
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest || !strings.Contains(err.Error(), "does not support color") {
		t.Errorf("colour on a plug: %v", err)
	}
	if _, err := c.Do(ctx, http.MethodGet, hue.ResourcePath+"light/no-such-id", nil); !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Errorf("missing: %v", err)
	}
	f.removeKey(fakeKey)
	if _, err := c.List(ctx, "light"); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("removed key: %v", err)
	}
	if _, err := c.Events(ctx); err == nil {
		t.Error("event stream with a removed key")
	}
}

func TestClientEvents(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	c := hue.NewClient(hue.Config{Address: f.addr(), AppKey: fakeKey, TLS: fakeCheck(t, f)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body, err := c.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got := make(chan hue.Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- hue.ReadEvents(body, func(evs []hue.Event) {
			for _, e := range evs {
				got <- e
			}
		}, nil)
	}()
	for deadline := time.Now().Add(5 * time.Second); f.streamCount() != 1 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	f.emit(map[string]any{"id": "b1", "type": "button", "owner": map[string]any{"rid": "d1", "rtype": "device"},
		"button": map[string]any{"button_report": map[string]any{"event": "short_release"}}})
	select {
	case e := <-got:
		if e.Type != "update" || len(e.Data) != 1 || hue.LastButtonEvent(e.Data[0]) != "short_release" || e.Data[0].Owner() != "d1" {
			t.Errorf("event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	f.dropStreams()
	select {
	case err := <-done:
		if !errors.Is(err, hue.ErrStreamClosed) {
			t.Errorf("end: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
}

func TestClientPair(t *testing.T) {
	f := newFake(t, "ecb5fafffe0a0b0d", "ecb5fafffe0a0b0d")
	c := hue.NewClient(hue.Config{Address: f.addr(), TLS: fakeCheck(t, f)})
	ctx := context.Background()
	if _, err := c.Pair(ctx, "test#one"); !errors.Is(err, hue.ErrLinkButton) {
		t.Fatalf("not pressed: %v", err)
	}
	f.pressLinkButton()
	res, err := c.Pair(ctx, "test#one")
	if err != nil || res.AppKey == "" || len(res.ClientKey) != 32 {
		t.Fatalf("pressed: %+v %v", res, err)
	}
	f.mu.Lock()
	last := f.last
	f.mu.Unlock()
	if last["devicetype"] != "test#one" || last["generateclientkey"] != true {
		t.Errorf("pairing request %v", last)
	}
	k := hue.NewClient(hue.Config{Address: f.addr(), AppKey: res.AppKey, TLS: fakeCheck(t, f)})
	if _, err := k.List(ctx, "light"); err != nil {
		t.Errorf("with the new key: %v", err)
	}
	if _, err := c.Pair(ctx, ""); err == nil || !strings.Contains(err.Error(), "error 5") {
		t.Errorf("no devicetype: %v", err)
	}
}

func TestClientPairV1OverHTTP(t *testing.T) {
	v1 := newFakeV1(t)
	c := hue.NewClient(hue.Config{Address: v1.addr(), Timeout: 2 * time.Second})
	if _, err := c.Pair(context.Background(), "test#v1"); !errors.Is(err, hue.ErrLinkButton) {
		t.Fatalf("not pressed: %v", err)
	}
	v1.mu.Lock()
	v1.link = true
	v1.mu.Unlock()
	res, err := c.Pair(context.Background(), "test#v1")
	if err != nil || res.AppKey != fakeV1Key || res.ClientKey != "" {
		t.Fatalf("pressed: %+v %v", res, err)
	}
}

func TestClientTLSCheck(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	other := newFake(t, "001788fffe000001", "someone-else")
	ctx := context.Background()
	list := func(addr string, chk hue.TLSCheck) error {
		c := hue.NewClient(hue.Config{Address: addr, AppKey: fakeKey, TLS: chk, Timeout: 3 * time.Second})
		defer c.CloseIdleConnections()
		_, err := c.List(ctx, "light")
		return err
	}
	var mu sync.Mutex
	pins := map[string]string{}
	chk := fakeCheck(t, f)
	chk.Pin = func(id, fp string) error {
		mu.Lock()
		defer mu.Unlock()
		if old := pins[id]; old != "" && old != fp {
			return fmt.Errorf("bridge %s changed its certificate", id)
		}
		pins[id] = fp
		return nil
	}
	if err := list(f.addr(), chk); err != nil {
		t.Fatalf("verified bridge: %v", err)
	}
	if len(pins[f.id]) != 64 {
		t.Errorf("pins %v", pins)
	}
	// Another certificate under the same bridge ID: the pin refuses it.
	if err := list(other.addr(), hue.TLSCheck{BridgeID: func() string { return f.id }, Pin: chk.Pin, Unverified: func(string, string, []string) error { return nil }}); err == nil ||
		!strings.Contains(err.Error(), "changed its certificate") {
		t.Errorf("changed certificate: %v", err)
	}
	// A common name other than the bridge ID: refused by default.
	if err := list(other.addr(), hue.TLSCheck{BridgeID: func() string { return other.id }}); err == nil || !strings.Contains(err.Error(), "is not the bridge ID") {
		t.Errorf("default policy: %v", err)
	}
	// The callback sees both problems and may accept.
	var problems []string
	var subject string
	accept := hue.TLSCheck{BridgeID: func() string { return other.id }, Roots: f.roots(t),
		Unverified: func(s, _ string, p []string) error { subject, problems = s, p; return nil }}
	if err := list(other.addr(), accept); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if subject != other.id || len(problems) != 2 || !strings.Contains(problems[0], `"someone-else"`) || !strings.Contains(problems[1], "trusted roots") {
		t.Errorf("subject %s problems %q", subject, problems)
	}
	// No bridge ID known: the zero check refuses.
	if err := list(f.addr(), hue.TLSCheck{}); err == nil {
		t.Error("an unknown bridge ID was accepted by the zero check")
	}
}

func TestClientReadConfig(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	v1 := newFakeV1(t)
	ctx := context.Background()
	hc := &http.Client{Timeout: 3 * time.Second}
	cfg, err := hue.ReadV1Config(ctx, hc, v1.addr())
	if err != nil || cfg.BridgeID != "001788FFFE1A2B3C" || cfg.ModelID != "BSB001" || !hue.NeedsV1(cfg) {
		t.Errorf("v1 config %+v %v", cfg, err)
	}
	c := hue.NewClient(hue.Config{Address: f.addr(), TLS: fakeCheck(t, f)})
	cfg, err = hue.ReadConfig(ctx, c.HTTPClient(), c.BaseURL())
	if err != nil || hue.NormaliseBridgeID(cfg.BridgeID) != f.id || hue.NeedsV1(cfg) {
		t.Errorf("https config %+v %v", cfg, err)
	}
	if _, err := hue.ReadV1Config(ctx, hc, "127.0.0.1:1"); err == nil {
		t.Error("nothing listening: no error")
	}
}

func TestClientV1(t *testing.T) {
	v1 := newFakeV1(t)
	ctx := context.Background()
	c := hue.NewClient(hue.Config{Address: v1.addr()})
	c.V1 = hue.NewV1Client(v1.addr(), fakeV1Key, nil)
	lights, err := c.List(ctx, "light")
	if err != nil || len(lights) != 1 || lights[0].ID() != "v1-light-1-light" {
		t.Fatalf("lights %v %v", lights, err)
	}
	// The light service carries its name and archetype in metadata, as a
	// CLIP v2 light does (found missing against diyHue's API v1).
	if lights[0].Str("metadata", "name") != "Sofa lamp" || lights[0].Str("metadata", "archetype") != "sultan_bulb" {
		t.Errorf("light metadata %v", lights[0]["metadata"])
	}
	rooms, _ := c.List(ctx, "room")
	if len(rooms) != 1 || rooms[0].Str("metadata", "name") != "Living room" {
		t.Errorf("rooms %v", rooms)
	}
	if err := c.Put(ctx, "light", "v1-light-1-light", map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 100.0}}); err != nil {
		t.Fatal(err)
	}
	v1.mu.Lock()
	st := v1.state["lights"].(map[string]any)["1"].(map[string]any)["state"].(map[string]any)
	on, bri := st["on"], st["bri"]
	v1.mu.Unlock()
	if on != true || bri != 254.0 {
		t.Errorf("light state on=%v bri=%v", on, bri)
	}
	scenes, _ := c.List(ctx, "scene")
	if len(scenes) != 2 || scenes[1].Str("metadata", "name") != "Relax" {
		t.Fatalf("scenes %v", scenes)
	}
	if err := c.Put(ctx, "scene", scenes[1].ID(), map[string]any{"recall": map[string]any{"action": "active"}}); err != nil {
		t.Fatal(err)
	}
	v1.mu.Lock()
	lastPut := v1.puts[len(v1.puts)-1]
	v1.mu.Unlock()
	if lastPut != "/groups/1/action" {
		t.Errorf("scene recall went to %s", lastPut)
	}
	if _, err := c.Events(ctx); err == nil {
		t.Error("API v1 has an event stream")
	}
	if err := hue.NewV1Client(v1.addr(), "wrong", nil).Refresh(ctx); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
	var ae *hue.APIError
	if _, err := hue.NewV1Client(v1.addr(), fakeV1Key, nil).Request(ctx, http.MethodPut, "/lights/1/state", map[string]any{"bri": 900}); !errors.As(err, &ae) || !strings.Contains(err.Error(), "invalid value") {
		t.Errorf("bad value: %v", err)
	}
}
