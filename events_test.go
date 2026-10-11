// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// Covers: SIG-IPE-4378
func TestHueEventStreamFraming(t *testing.T) {
	in := ": hi\n\n" +
		"id: 1700000000:0\n" +
		`data: [{"creationtime":"2026-09-25T04:00:00Z","id":"e1","type":"update","data":[{"id":"l1","type":"light","owner":{"rid":"d1","rtype":"device"},"on":{"on":false}}]}]` + "\n\n" +
		"id: 1700000000:1\n" +
		`data: [{"creationtime":"2026-09-25T04:00:01Z","id":"e2","type":"update",` + "\n" +
		`data: "data":[{"id":"b1","type":"button","button":{"last_event":"short_release"}}]}]` + "\n\n"
	var got []Event
	err := ReadEvents(strings.NewReader(in), func(evs []Event) { got = append(got, evs...) }, nil)
	if err == nil || len(got) != 2 {
		t.Fatalf("events = %+v, err = %v", got, err)
	}
	if got[0].Type != "update" || got[0].Data[0].Owner() != "d1" || got[0].Created.IsZero() {
		t.Errorf("first = %+v", got[0])
	}
	if LastButtonEvent(got[1].Data[0]) != "short_release" {
		t.Errorf("second = %+v", got[1])
	}
	// Merging an update keeps the rest of the resource.
	cur := map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 20.0}}
	Merge(cur, map[string]any{"dimming": map[string]any{"brightness": 40.0}})
	if cur["on"].(map[string]any)["on"] != true || cur["dimming"].(map[string]any)["brightness"] != 40.0 {
		t.Errorf("merged = %v", cur)
	}
}

func TestReadEventsEnds(t *testing.T) {
	if err := ReadEvents(strings.NewReader("data: [not json\n\n"), func([]Event) { t.Error("bad JSON handed out") }, nil); !errors.Is(err, ErrStreamClosed) {
		t.Errorf("end of stream: %v", err)
	}
	stop := make(chan struct{}, 1)
	stop <- struct{}{}
	n := 0
	err := ReadEvents(strings.NewReader("data: []\n\ndata: []\n\n"), func([]Event) { n++ }, stop)
	if !errors.Is(err, ErrStopped) {
		t.Errorf("stop: %v", err)
	}
	pr, pw := io.Pipe()
	_ = pw.CloseWithError(errors.New("link down"))
	if err := ReadEvents(pr, func([]Event) {}, nil); err == nil || err.Error() != "link down" {
		t.Errorf("read error: %v", err)
	}
}

func TestResource(t *testing.T) {
	r := Resource{"id": "x", "type": "light", "owner": map[string]any{"rid": "d", "rtype": "device"},
		"dimming": map[string]any{"brightness": 12.5}, "on": map[string]any{"on": true},
		"services": []any{map[string]any{"rid": "a", "rtype": "light"}, map[string]any{"rtype": "no-rid"}}}
	if r.ID() != "x" || r.Type() != "light" || r.Owner() != "d" {
		t.Errorf("ids %v", r)
	}
	if f, ok := r.Num("dimming", "brightness"); !ok || f != 12.5 {
		t.Errorf("num %v %v", f, ok)
	}
	if b, ok := r.Bool("on", "on"); !ok || !b {
		t.Errorf("bool %v %v", b, ok)
	}
	if _, ok := r.Num("on", "on", "deeper"); ok {
		t.Error("a path through a boolean")
	}
	if refs := r.Refs("services"); len(refs) != 1 || refs[0] != (Ref{"a", "light"}) {
		t.Errorf("refs %v", refs)
	}
	c := r.Clone()
	c["dimming"].(map[string]any)["brightness"] = 99.0
	if f, _ := r.Num("dimming", "brightness"); f != 12.5 {
		t.Error("clone shares state")
	}
	if LastButtonEvent(Resource{"button": map[string]any{"last_event": "repeat", "button_report": map[string]any{"event": "long_press"}}}) != "long_press" {
		t.Error("the report's event comes first")
	}
}

func TestConversions(t *testing.T) {
	// light_level is 10000·log10(lux)+1: 1 lx → 1, 10 lx → 10001, 1000 lx → 30001.
	for lv, lux := range map[float64]float64{1: 1, 10001: 10, 30001: 1000, 0: 0, -5: 0} {
		if got := LuxFromLightLevel(lv); got != lux {
			t.Errorf("LuxFromLightLevel(%v) = %v, want %v", lv, got, lux)
		}
	}
	if NormaliseBridgeID(" 00:17:88:FF:FE:4A:1B:2C ") != "001788fffe4a1b2c" {
		t.Error("bridge ID not normalised")
	}
	for in, want := range map[string]string{"192.168.1.2": "https://192.168.1.2", "127.0.0.1:8443": "https://127.0.0.1:8443",
		"fe80::1": "https://[fe80::1]", "https://hue.local/": "https://hue.local"} {
		if got := BaseURL(in); got != want {
			t.Errorf("BaseURL(%s) = %s", in, got)
		}
	}
	for in, want := range map[string]string{"192.168.1.2": "http://192.168.1.2", "https://h:80/": "http://h:80", "fe80::1": "http://[fe80::1]"} {
		if got := V1Base(in); got != want {
			t.Errorf("V1Base(%s) = %s", in, got)
		}
	}
	type be struct {
		button int
		event  string
	}
	for in, want := range map[[2]any]be{{1002, "ZLLSwitch"}: {1, "short_release"}, {4003, "ZLLSwitch"}: {4, "long_release"},
		{2001, "ZLLSwitch"}: {2, "repeat"}, {3000, "ZLLSwitch"}: {3, "initial_press"}, {34, "ZGPSwitch"}: {1, "short_release"},
		{18, "ZGPSwitch"}: {4, "short_release"}, {5, "ZLLSwitch"}: {1, ""}} {
		b, e := V1ButtonEvent(in[0].(int), in[1].(string))
		if b != want.button || e != want.event {
			t.Errorf("V1ButtonEvent(%v) = %d %q", in, b, e)
		}
	}
	if !NeedsV1(V1Config{ModelID: "BSB001", APIVersion: "1.16.0"}) || NeedsV1(V1Config{ModelID: "BSB002", APIVersion: "1.60.0"}) ||
		!NeedsV1(V1Config{ModelID: "BSB002", APIVersion: "1.30.0"}) || NeedsV1(V1Config{}) {
		t.Error("NeedsV1")
	}
	b := V1State(map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 0.1},
		"color": map[string]any{"xy": map[string]any{"x": 0.3, "y": 0.4}}, "color_temperature": map[string]any{"mirek": 300},
		"dynamics": map[string]any{"duration": 250}})
	if b["bri"] != 1 || b["transitiontime"] != 3 || b["on"] != true || b["ct"] != 300 {
		t.Errorf("v1 state %v", b)
	}
	if xy, _ := b["xy"].([]any); len(xy) != 2 || xy[0] != 0.3 {
		t.Errorf("v1 xy %v", b["xy"])
	}
	if err := Scrub(errors.New("GET https://h/api/secret-key/x failed"), "secret-key"); err.Error() != "GET https://h/api/(hidden)/x failed" {
		t.Errorf("scrub %v", err)
	}
}
