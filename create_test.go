// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/hue"
)

func TestCreateUpdateDelete(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	c := hue.NewClient(hue.Config{Address: f.addr(), AppKey: fakeKey, TLS: fakeCheck(t, f), Timeout: 5 * time.Second})
	defer c.CloseIdleConnections()
	ctx := context.Background()

	room, err := c.CreateRoom(ctx, hue.GroupSpec{Name: "Study", Archetype: "office", Children: []hue.Ref{{RID: "dev-1", RType: "device"}}})
	if err != nil || room.RType != "room" || room.RID == "" {
		t.Fatalf("room %+v %v", room, err)
	}
	got, err := c.Get(ctx, "room", room.RID)
	if err != nil || got.Str("metadata", "name") != "Study" || got.Str("metadata", "archetype") != "office" || len(got.Refs("children")) != 1 {
		t.Fatalf("room read back %v %v", got, err)
	}
	// The bridge's refusal comes back as an *APIError.
	var ae *hue.APIError
	if _, err := c.CreateRoom(ctx, hue.GroupSpec{Name: "Ghost", Children: []hue.Ref{{RID: "no-device", RType: "device"}}}); !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Errorf("bad child: %v", err)
	}
	if _, err := c.CreateRoom(ctx, hue.GroupSpec{}); err == nil {
		t.Error("a room without a name was sent")
	}
	zone, err := c.CreateZone(ctx, hue.GroupSpec{Name: "Reading corner", Children: []hue.Ref{{RID: "light-1", RType: "light"}}})
	if err != nil || zone.RType != "zone" {
		t.Fatalf("zone %+v %v", zone, err)
	}
	if err := c.UpdateGroup(ctx, "zone", zone.RID, hue.GroupSpec{Name: "Corner"}); err != nil {
		t.Fatal(err)
	}
	speed := 0.5
	scene, err := c.CreateScene(ctx, hue.SceneSpec{Name: "Evening", Group: room, Speed: &speed,
		Actions: []hue.SceneAction{{Target: hue.Ref{RID: "light-1", RType: "light"}, Action: map[string]any{"on": map[string]any{"on": true}}}}})
	if err != nil || scene.RType != "scene" {
		t.Fatalf("scene %+v %v", scene, err)
	}
	if err := c.UpdateScene(ctx, scene.RID, hue.SceneSpec{Name: "Late evening", Group: room}); err != nil {
		t.Fatal(err)
	}
	if err := c.RecallScene(ctx, scene.RID, ""); err != nil {
		t.Fatal(err)
	}
	bh, err := c.CreateBehavior(ctx, hue.BehaviorSpec{Name: "Wake up", ScriptID: "ff8957e3-2eb9-4699-a0c8-ad2cb3ede704", Enabled: true,
		Configuration: map[string]any{"when": map[string]any{"time_point": map[string]any{"type": "time", "time": map[string]any{"hour": 7.0, "minute": 0.0}}}}})
	if err != nil || bh.RType != "behavior_instance" {
		t.Fatalf("behaviour %+v %v", bh, err)
	}
	if _, err := c.CreateBehavior(ctx, hue.BehaviorSpec{Name: "No script"}); err == nil {
		t.Error("a behaviour without a script was sent")
	}
	if err := c.UpdateBehavior(ctx, bh.RID, hue.BehaviorSpec{Enabled: false}); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	posts, puts := f.posts, f.puts
	f.mu.Unlock()
	want := []string{"room", "room", "zone", "scene", "behavior_instance"}
	if len(posts) != len(want) {
		t.Fatalf("posts %v", posts)
	}
	for i, p := range posts {
		if p["rtype"] != want[i] {
			t.Errorf("post %d: %v", i, p)
		}
	}
	sb := posts[3]["body"].(map[string]any)
	if sb["speed"] != 0.5 || sb["group"].(map[string]any)["rid"] != room.RID || len(sb["actions"].([]any)) != 1 {
		t.Errorf("scene body %v", sb)
	}
	if bb := posts[4]["body"].(map[string]any); bb["script_id"] == nil || bb["enabled"] != true {
		t.Errorf("behaviour body %v", bb)
	}
	// Updates send only what was set: the scene update has no group, the
	// zone update no children.
	if len(puts) != 4 {
		t.Fatalf("puts %v", puts)
	}
	if _, ok := puts[0]["children"]; ok {
		t.Errorf("zone update %v", puts[0])
	}
	if _, ok := puts[1]["group"]; ok {
		t.Errorf("scene update %v", puts[1])
	}
	if !reflect.DeepEqual(puts[2], map[string]any{"recall": map[string]any{"action": "active"}}) {
		t.Errorf("recall %v", puts[2])
	}
	if !reflect.DeepEqual(puts[3], map[string]any{"enabled": false}) {
		t.Errorf("behaviour update %v", puts[3])
	}

	for _, r := range []hue.Ref{scene, zone, room, bh} {
		if err := c.Delete(ctx, r.RType, r.RID); err != nil {
			t.Errorf("delete %v: %v", r, err)
		}
	}
	if err := c.Delete(ctx, "room", room.RID); !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Errorf("delete twice: %v", err)
	}
	if _, err := c.Get(ctx, "room", room.RID); !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Errorf("deleted room read: %v", err)
	}
}

func TestApplicationIDAndEntertainment(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	c := hue.NewClient(hue.Config{Address: f.addr(), AppKey: fakeKey, TLS: fakeCheck(t, f)})
	ctx := context.Background()
	id, err := c.ApplicationID(ctx)
	if err != nil || id != "app-id-of-"+fakeKey {
		t.Fatalf("application ID %q %v", id, err)
	}
	ref, err := c.Post(ctx, "entertainment_configuration", map[string]any{"metadata": map[string]any{"name": "TV"}, "configuration_type": "screen"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartEntertainment(ctx, ref.RID); err != nil {
		t.Fatal(err)
	}
	if err := c.StopEntertainment(ctx, ref.RID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	puts := f.puts
	f.mu.Unlock()
	if len(puts) != 2 || puts[0]["action"] != "start" || puts[1]["action"] != "stop" {
		t.Errorf("puts %v", puts)
	}
	bad := hue.NewClient(hue.Config{Address: f.addr(), AppKey: "wrong", TLS: fakeCheck(t, f)})
	if _, err := bad.ApplicationID(ctx); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
}

func TestTypedModels(t *testing.T) {
	f := newFake(t, "001788fffe4a1b2c", "001788fffe4a1b2c")
	c := hue.NewClient(hue.Config{Address: f.addr(), AppKey: fakeKey, TLS: fakeCheck(t, f)})
	lights, err := hue.ListAs[hue.Light](context.Background(), c, "light")
	if err != nil || len(lights) != 2 {
		t.Fatalf("lights %v %v", lights, err)
	}
	l := lights[0]
	if l.ID != "light-1" || l.Owner.Ref() != (hue.Ref{RID: "dev-1", RType: "device"}) || l.Metadata.Name != "Sofa lamp" || !l.On.On || l.Color == nil || l.Color.XY.X != 0.3 {
		t.Errorf("light %+v", l)
	}
	if lights[1].Color != nil || lights[1].On.On {
		t.Errorf("plug %+v", lights[1])
	}
	sc, err := hue.As[hue.Scene](hue.Resource{"id": "s1", "type": "scene", "metadata": map[string]any{"name": "Relax"},
		"group": map[string]any{"rid": "r1", "rtype": "room"}, "status": map[string]any{"active": "static"},
		"actions": []any{map[string]any{"target": map[string]any{"rid": "l1", "rtype": "light"}, "action": map[string]any{"on": map[string]any{"on": true}}}}})
	if err != nil || sc.Group.RID != "r1" || sc.Status.Active != "static" || len(sc.Actions) != 1 || sc.Actions[0].Target.RID != "l1" {
		t.Errorf("scene %+v %v", sc, err)
	}
	ec, err := hue.As[hue.EntertainmentConfiguration](hue.Resource{"id": "e1", "type": "entertainment_configuration", "status": "active",
		"channels":  []any{map[string]any{"channel_id": 0.0, "position": map[string]any{"x": -1.0, "y": 0.5, "z": 0.0}, "members": []any{map[string]any{"service": map[string]any{"rid": "x", "rtype": "entertainment"}, "index": 0.0}}}},
		"locations": map[string]any{"service_locations": []any{map[string]any{"service": map[string]any{"rid": "x", "rtype": "entertainment"}, "positions": []any{map[string]any{"x": -1.0, "y": 0.5, "z": 0.0}}}}}})
	if err != nil || ec.Status != "active" || len(ec.Channels) != 1 || ec.Channels[0].Position.X != -1 || len(ec.Locations.ServiceLocations) != 1 {
		t.Errorf("entertainment configuration %+v %v", ec, err)
	}
	if _, err := hue.As[hue.Light](hue.Resource{"on": "not an object"}); err == nil {
		t.Error("a wrong shape converted")
	}
}

func TestRootsFromPEM(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mk := func(ca bool) []byte {
		tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "demo root-bridge"}, NotBefore: time.Now().Add(-time.Hour),
			NotAfter: time.Now().Add(time.Hour), IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	two := append(mk(true), mk(true)...)
	pool, err := hue.RootsFromPEM(two)
	if err != nil || pool == nil {
		t.Fatalf("two roots: %v", err)
	}
	if _, err := hue.RootsFromPEM([]byte("nothing here")); err == nil {
		t.Error("no certificate: no error")
	}
	if _, err := hue.RootsFromPEM(mk(false)); err == nil || !strings.Contains(err.Error(), "not a CA") {
		t.Errorf("leaf as root: %v", err)
	}
	path := filepath.Join(t.TempDir(), "hue-root.pem")
	if err := os.WriteFile(path, two, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := hue.LoadRoots(path); err != nil {
		t.Errorf("load: %v", err)
	}
	if _, err := hue.LoadRoots(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("missing file: no error")
	}
}

// Light groups, entertainment groups and light scenes of API v1 are
// served (they were skipped before v0.2.0), and rooms, zones and scenes
// can be created, changed and deleted through CLIP v2 requests.
func TestClientV1GroupsAndChanges(t *testing.T) {
	v1 := newFakeV1(t)
	ctx := context.Background()
	c := hue.NewClient(hue.Config{Address: v1.addr()})
	c.V1 = hue.NewV1Client(v1.addr(), fakeV1Key, nil)

	rooms, _ := c.List(ctx, "room")
	if len(rooms) != 1 || rooms[0].Str("metadata", "archetype") != "living_room" {
		t.Errorf("rooms %v", rooms)
	}
	lg, _ := c.List(ctx, "light_group")
	if len(lg) != 1 || lg[0].Str("metadata", "name") != "Wake-up lights" || lg[0].Str("v1_type") != "LightGroup" || len(lg[0].Refs("children")) != 1 {
		t.Errorf("light groups %v", lg)
	}
	ents, _ := c.List(ctx, "entertainment_configuration")
	if len(ents) != 1 {
		t.Fatalf("entertainment %v", ents)
	}
	ec, _ := hue.As[hue.EntertainmentConfiguration](ents[0])
	if ec.Status != "inactive" || ec.ConfigurationType != "screen" || len(ec.Channels) != 1 || ec.Channels[0].Position.X != -0.5 || len(ec.LightServices) != 1 {
		t.Errorf("entertainment %+v", ec)
	}
	homes, _ := c.List(ctx, "bridge_home")
	if len(homes) != 1 || len(homes[0].Refs("children")) != 1 {
		t.Errorf("home %v", homes)
	}
	scenes, _ := c.List(ctx, "scene")
	if len(scenes) != 2 || scenes[0].Str("group", "rtype") != "bridge_home" || len(scenes[0]["actions"].([]any)) != 1 {
		t.Fatalf("scenes %v", scenes)
	}
	// A light scene is recalled through group 0.
	if err := c.RecallScene(ctx, scenes[0].ID(), ""); err != nil {
		t.Fatal(err)
	}
	// The light group's grouped light and the home's switch lights.
	gls, _ := c.List(ctx, "grouped_light")
	if len(gls) != 3 {
		t.Fatalf("grouped lights %v", gls)
	}
	for _, gl := range gls {
		if err := c.Put(ctx, "grouped_light", gl.ID(), map[string]any{"on": map[string]any{"on": true}}); err != nil {
			t.Errorf("grouped light %s: %v", gl.ID(), err)
		}
	}
	if err := c.StartEntertainment(ctx, ec.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateGroup(ctx, "room", rooms[0].ID(), hue.GroupSpec{Name: "Lounge", Archetype: "living_room"}); err != nil {
		t.Fatal(err)
	}
	room, err := c.CreateRoom(ctx, hue.GroupSpec{Name: "Study", Archetype: "office", Children: []hue.Ref{{RID: "v1-light-1", RType: "device"}}})
	if err != nil || room.RType != "room" || !strings.HasPrefix(room.RID, "v1-group-") {
		t.Fatalf("room %+v %v", room, err)
	}
	zone, err := c.CreateZone(ctx, hue.GroupSpec{Name: "Corner", Children: []hue.Ref{{RID: "v1-light-1-light", RType: "light"}}})
	if err != nil || zone.RType != "zone" {
		t.Fatalf("zone %+v %v", zone, err)
	}
	scene, err := c.CreateScene(ctx, hue.SceneSpec{Name: "Dim", Group: rooms[0].Ref(),
		Actions: []hue.SceneAction{{Target: hue.Ref{RID: "v1-light-1-light", RType: "light"}, Action: map[string]any{"dimming": map[string]any{"brightness": 50.0}}}}})
	if err != nil || !strings.HasPrefix(scene.RID, "v1-scene-") {
		t.Fatalf("scene %+v %v", scene, err)
	}
	var ae *hue.APIError
	if _, err := c.CreateBehavior(ctx, hue.BehaviorSpec{ScriptID: "x"}); !errors.As(err, &ae) || ae.Status != http.StatusMethodNotAllowed {
		t.Errorf("behaviour on v1: %v", err)
	}
	if _, err := c.Get(ctx, "room", room.RID); err != nil {
		t.Errorf("new room read: %v", err)
	}
	if err := c.Delete(ctx, "scene", scene.RID); err != nil {
		t.Error(err)
	}
	if err := c.Delete(ctx, "room", room.RID); err != nil {
		t.Error(err)
	}
	v1.mu.Lock()
	puts, bodies := append([]string(nil), v1.puts...), v1.bodies
	v1.mu.Unlock()
	want := []string{"/groups/0/action", "/groups/1/action", "/groups/2/action", "/groups/0/action", "/groups/3",
		"/groups/1", "POST /groups", "POST /groups", "POST /scenes", "/scenes/12/lightstates/1", "DELETE /scenes/12", "DELETE /groups/13"}
	// The grouped lights come in group order, the home (group 0) last.
	if !reflect.DeepEqual(puts, want) {
		t.Errorf("v1 requests\n got %v\nwant %v", puts, want)
	}
	if len(bodies) >= 5 {
		if bodies[0]["stream"].(map[string]any)["active"] != true {
			t.Errorf("stream start %v", bodies[0])
		}
		if bodies[1]["name"] != "Lounge" || bodies[1]["class"] != "Living room" {
			t.Errorf("room update %v", bodies[1])
		}
		if bodies[2]["type"] != "Room" || bodies[2]["class"] != "Office" || !reflect.DeepEqual(bodies[2]["lights"], []any{"1"}) {
			t.Errorf("room create %v", bodies[2])
		}
		if bodies[3]["type"] != "Zone" || !reflect.DeepEqual(bodies[3]["lights"], []any{"1"}) {
			t.Errorf("zone create %v", bodies[3])
		}
		if bodies[4]["type"] != "GroupScene" || bodies[4]["group"] != "1" {
			t.Errorf("scene create %v", bodies[4])
		}
	}
}
