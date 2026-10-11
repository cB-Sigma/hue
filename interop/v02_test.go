// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/cB-Sigma/hue"
	"github.com/cB-Sigma/hue/entertainment"
)

// The v0.2.0 additions against diyHue: creating, changing and deleting
// rooms, zones, scenes and behaviours, typed views, the application ID,
// and entertainment streaming to diyHue's DTLS endpoint (an openssl
// s_server it starts per stream).

// findByName lists a type and returns the resource with that name.
func findByName(t *testing.T, c *hue.Client, rtype, name string) hue.Resource {
	t.Helper()
	list, err := c.List(context.Background(), rtype)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.Str("metadata", "name") == name {
			return r
		}
	}
	return nil
}

func TestCreateDelete(t *testing.T) {
	p := need(t)
	c := client(t, p)
	ctx := context.Background()
	l := lamp(t, c)
	room, err := c.CreateRoom(ctx, hue.GroupSpec{Name: "Interop study", Archetype: "office", Children: []hue.Ref{{RID: l.Owner(), RType: "device"}}})
	if err != nil {
		t.Fatalf("room: %v", err)
	}
	t.Logf("room answered as %+v", room)
	listed := findByName(t, c, "room", "Interop study")
	if listed == nil {
		t.Fatal("the new room is not listed")
	}
	if listed.ID() != room.RID {
		// Recorded, not judged: diyHue lists a room under another ID than
		// the one its POST answer gives; both are accepted by its PUT,
		// GET and DELETE.
		t.Logf("diyHue lists the room as %s, its POST answered %s", listed.ID(), room.RID)
	}
	if got, err := c.Get(ctx, "room", room.RID); err != nil || got.Str("metadata", "name") != "Interop study" {
		t.Errorf("room by the POST's ID: %v %v", got, err)
	}
	g, err := hue.As[hue.Group](listed)
	if err != nil || len(g.Children) != 1 {
		t.Errorf("room %+v %v", g, err)
	}
	if err := c.UpdateGroup(ctx, "room", listed.ID(), hue.GroupSpec{Name: "Interop office"}); err != nil {
		t.Errorf("rename: %v", err)
	} else if findByName(t, c, "room", "Interop office") == nil {
		t.Error("the rename is not listed")
	}
	zone, err := c.CreateZone(ctx, hue.GroupSpec{Name: "Interop corner", Archetype: "lounge", Children: []hue.Ref{{RID: l.ID(), RType: "light"}}})
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	scene, err := c.CreateScene(ctx, hue.SceneSpec{Name: "Interop film", Group: hue.Ref{RID: listed.ID(), RType: "room"},
		Actions: []hue.SceneAction{{Target: hue.Ref{RID: l.ID(), RType: "light"}, Action: map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 30.0}}}}})
	if err != nil {
		t.Fatalf("scene: %v", err)
	}
	if findByName(t, c, "scene", "Interop film") == nil {
		t.Error("the new scene is not listed")
	}
	if err := c.RecallScene(ctx, scene.RID, ""); err != nil {
		t.Errorf("recall: %v", err)
	}
	scripts, err := c.List(ctx, "behavior_script")
	if err != nil || len(scripts) == 0 {
		t.Fatalf("behaviour scripts %v %v", scripts, err)
	}
	bh, err := c.CreateBehavior(ctx, hue.BehaviorSpec{Name: "Interop timer", ScriptID: scripts[0].ID(), Enabled: true, Configuration: map[string]any{}})
	if err != nil {
		t.Fatalf("behaviour: %v", err)
	}
	if err := c.UpdateBehavior(ctx, bh.RID, hue.BehaviorSpec{Name: "Interop timer", Enabled: false}); err != nil {
		t.Errorf("behaviour update: %v", err)
	}
	if b := findByName(t, c, "behavior_instance", "Interop timer"); b == nil {
		t.Error("the behaviour is not listed")
	} else if en, _ := b.Bool("enabled"); en {
		t.Errorf("behaviour still enabled: %v", b)
	}
	for _, r := range []hue.Ref{bh, scene, zone, room} {
		if err := c.Delete(ctx, r.RType, r.RID); err != nil {
			t.Errorf("delete %s: %v", r.RType, err)
		}
	}
	for _, x := range [][2]string{{"room", "Interop office"}, {"zone", "Interop corner"}, {"scene", "Interop film"}, {"behavior_instance", "Interop timer"}} {
		if findByName(t, c, x[0], x[1]) != nil {
			t.Errorf("%s %s still listed after DELETE", x[0], x[1])
		}
	}
}

func TestEntertainment(t *testing.T) {
	p := need(t)
	c := client(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := lamp(t, c)
	appID, err := c.ApplicationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("application ID %q (diyHue answers with the application key)", appID)
	// The lamp's entertainment service.
	ents, err := c.List(ctx, "entertainment")
	if err != nil {
		t.Fatal(err)
	}
	var svc string
	for _, e := range ents {
		if e.Owner() == l.Owner() {
			svc = e.ID()
		}
	}
	if svc == "" {
		t.Fatalf("no entertainment service for the lamp among %v", ents)
	}
	ref, err := c.Post(ctx, "entertainment_configuration", map[string]any{"metadata": map[string]any{"name": "Interop TV"}, "configuration_type": "screen",
		"locations": map[string]any{"service_locations": []any{map[string]any{"service": map[string]any{"rid": svc, "rtype": "entertainment"},
			"positions": []any{map[string]any{"x": 0.0, "y": 0.8, "z": 0.0}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Delete(context.Background(), "entertainment_configuration", ref.RID) }()
	listed := findByName(t, c, "entertainment_configuration", "Interop TV")
	if listed == nil {
		t.Fatal("the area is not listed")
	}
	area, err := hue.As[hue.EntertainmentConfiguration](listed)
	if err != nil || len(area.Channels) != 1 {
		t.Fatalf("area %+v %v", area, err)
	}
	s, err := entertainment.Open(ctx, c, area.ID, p.pair.ClientKey, entertainment.Options{Addr: p.udpAddr, Space: entertainment.XY})
	if err != nil {
		logs, _ := docker("logs", "--tail", "30", the.name)
		t.Fatalf("open: %v\n%s", err, logs)
	}
	defer func() { _ = s.Close(context.Background()) }()
	if a := findByName(t, c, "entertainment_configuration", "Interop TV"); a.Str("status") != "active" {
		t.Errorf("status %v", a["status"])
	}
	// Frames of one colour, many times a second, as a streaming client
	// sends them; diyHue reads the stream in frame-sized pieces.
	const x, y, bri = 0.6, 0.35, 0.8
	deadline := time.Now().Add(20 * time.Second)
	var state struct {
		On  bool      `json:"on"`
		XY  []float64 `json:"xy"`
		Bri float64   `json:"bri"`
	}
	for time.Now().Before(deadline) {
		for i := 0; i < 25; i++ {
			if err := s.Send(entertainment.XYChannel(uint8(area.Channels[0].ChannelID), x, y, bri)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
		raw, err := hue.NewV1Client(p.httpAddr, p.pair.AppKey, nil).Request(ctx, http.MethodGet, "/lights", nil)
		if err != nil {
			t.Fatal(err)
		}
		var all map[string]struct {
			Name  string          `json:"name"`
			State json.RawMessage `json:"state"`
		}
		_ = json.Unmarshal(raw, &all)
		for _, v := range all {
			if v.Name == "Interop lamp" {
				_ = json.Unmarshal(v.State, &state)
			}
		}
		if state.On && len(state.XY) == 2 && math.Abs(state.XY[0]-x) < 0.01 && math.Abs(state.XY[1]-y) < 0.01 {
			t.Logf("diyHue applied the stream: %+v", state)
			break
		}
	}
	if !(state.On && len(state.XY) == 2 && math.Abs(state.XY[0]-x) < 0.01 && math.Abs(state.XY[1]-y) < 0.01) {
		logs, _ := docker("logs", "--tail", "40", the.name)
		t.Fatalf("the lamp did not take the streamed colour: %+v\n%s", state, logs)
	}
	if math.Abs(state.Bri-math.Floor(bri*65535/256)) > 2 {
		t.Logf("brightness %v (diyHue keeps the high byte of the 16-bit value)", state.Bri)
	}
	if err := s.Close(ctx); err != nil {
		t.Errorf("close: %v", err)
	}
	time.Sleep(time.Second)
	if a := findByName(t, c, "entertainment_configuration", "Interop TV"); a.Str("status") != "inactive" {
		t.Errorf("status after stop %v", a["status"])
	}
}
