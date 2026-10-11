// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cB-Sigma/hue"
)

func TestV1RulesSchedulesLinks(t *testing.T) {
	s, _ := newV1Store(t, "")
	a := hue.NewV1Client(s.addr(), storeKey, nil)
	ctx := context.Background()

	rules, err := a.Rules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != "1" || rules[0].TimesTriggered != 2 {
		t.Fatalf("rules %+v %v", rules, err)
	}
	if c := rules[0].Conditions; len(c) != 1 || c[0].Value != "1002" || c[0].Operator != "eq" {
		t.Errorf("a numeric condition value is read as text: %+v", c)
	}
	id, err := a.CreateRule(ctx, hue.V1Rule{Name: "Night light",
		Conditions: []hue.V1Condition{{Address: "/sensors/2/state/buttonevent", Operator: "eq", Value: "4002"}, {Address: "/sensors/2/state/lastupdated", Operator: "dx"}},
		Actions:    []hue.V1Action{{Address: "/groups/0/action", Method: "PUT", Body: map[string]any{"on": false}}}})
	if err != nil || id != "2" {
		t.Fatalf("create rule %q %v", id, err)
	}
	if _, err := a.CreateRule(ctx, hue.V1Rule{Name: "empty"}); err == nil {
		t.Error("a rule without conditions was sent")
	}
	r, err := a.Rule(ctx, id)
	if err != nil || r.Name != "Night light" || len(r.Conditions) != 2 || r.Actions[0].Body["on"] != false || r.ID != "2" {
		t.Fatalf("rule %+v %v", r, err)
	}
	if err := a.UpdateRule(ctx, id, hue.V1Rule{Status: "disabled"}); err != nil {
		t.Fatal(err)
	}
	if st := s.get("rules", "2", "status"); st != "disabled" {
		t.Errorf("status %v", st)
	}
	// An update sends only the fields set: no owner, no counters.
	if got := s.lastBody(); !reflect.DeepEqual(got, map[string]any{"status": "disabled"}) {
		t.Errorf("update body %v", got)
	}
	if err := a.DeleteRule(ctx, id); err != nil {
		t.Fatal(err)
	}
	_, err = a.Rule(ctx, id)
	var ae *hue.APIError
	if !errors.As(err, &ae) || !ae.HasCode(3) {
		t.Errorf("deleted rule: %v", err)
	}
	if err := a.DeleteRule(ctx, "../config"); err == nil {
		t.Error("an ID with a slash was sent")
	}

	no := false
	sid, err := a.CreateSchedule(ctx, hue.V1Schedule{Name: "Wake", Command: hue.V1Action{Address: "/api/" + storeKey + "/groups/0/action", Method: "PUT", Body: map[string]any{"on": true}},
		LocalTime: "W124/T07:00:00", Status: "enabled", AutoDelete: &no})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSchedule(ctx, hue.V1Schedule{Name: "no time", Command: hue.V1Action{Address: "/x", Method: "PUT"}}); err == nil {
		t.Error("a schedule without a time was sent")
	}
	if err := a.UpdateSchedule(ctx, sid, hue.V1Schedule{LocalTime: "W124/T07:30:00"}); err != nil {
		t.Fatal(err)
	}
	sch, err := a.Schedules(ctx)
	if err != nil || len(sch) != 1 || sch[0].LocalTime != "W124/T07:30:00" || sch[0].Command.Method != "PUT" || sch[0].AutoDelete == nil || *sch[0].AutoDelete {
		t.Fatalf("schedules %+v %v", sch, err)
	}
	one, err := a.Schedule(ctx, sid)
	if err != nil || one.Name != "Wake" || one.ID != sid {
		t.Errorf("schedule %+v %v", one, err)
	}

	lid, err := a.CreateResourceLink(ctx, hue.V1ResourceLink{Name: "My app", ClassID: 10010, Links: []string{"/rules/1", "/schedules/" + sid}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateResourceLink(ctx, lid, hue.V1ResourceLink{Links: []string{"/rules/1"}}); err != nil {
		t.Fatal(err)
	}
	links, err := a.ResourceLinks(ctx)
	if err != nil || len(links) != 1 || links[0].ClassID != 10010 || !reflect.DeepEqual(links[0].Links, []string{"/rules/1"}) {
		t.Fatalf("links %+v %v", links, err)
	}
	if l, err := a.ResourceLink(ctx, lid); err != nil || l.Name != "My app" {
		t.Errorf("link %+v %v", l, err)
	}
	for _, f := range []func() error{
		func() error { return a.DeleteResourceLink(ctx, lid) },
		func() error { return a.DeleteSchedule(ctx, sid) },
	} {
		if err := f(); err != nil {
			t.Error(err)
		}
	}
	want := []string{"POST /rules", "PUT /rules/2", "DELETE /rules/2", "POST /schedules", "PUT /schedules/1", "POST /resourcelinks", "PUT /resourcelinks/1",
		"DELETE /resourcelinks/1", "DELETE /schedules/1"}
	if got := s.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("requests\n got %v\nwant %v", got, want)
	}
}

func TestV1Sensors(t *testing.T) {
	s, _ := newV1Store(t, "")
	a := hue.NewV1Client(s.addr(), storeKey, nil)
	ctx := context.Background()
	id, err := a.CreateSensor(ctx, hue.V1Sensor{Name: "Away flag", Type: "CLIPGenericStatus", ModelID: "SIGMA01", SWVersion: "1.0",
		ManufacturerName: "Sigma", UniqueID: "sigma-away-1", State: map[string]any{"status": 0}})
	if err != nil || id != "1" {
		t.Fatalf("create %q %v", id, err)
	}
	if _, err := a.CreateSensor(ctx, hue.V1Sensor{Type: "CLIPGenericFlag"}); err == nil {
		t.Error("a sensor without a name was sent")
	}
	if err := a.SetSensorState(ctx, id, map[string]any{"status": 2}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSensorConfig(ctx, "2", map[string]any{"on": false}); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameSensor(ctx, id, "Holiday flag"); err != nil {
		t.Fatal(err)
	}
	all, err := a.Sensors(ctx)
	if err != nil || len(all) != 2 || all[0].ID != "1" || all[1].ID != "2" {
		t.Fatalf("sensors %+v %v", all, err)
	}
	if all[0].Name != "Holiday flag" || all[0].State["status"] != 2.0 || all[0].Type != "CLIPGenericStatus" {
		t.Errorf("CLIP sensor %+v", all[0])
	}
	if all[1].Config["on"] != false {
		t.Errorf("config %+v", all[1].Config)
	}
	one, err := a.Sensor(ctx, "2")
	if err != nil || one.ModelID != "RWL021" {
		t.Errorf("sensor %+v %v", one, err)
	}
	// The search: started, then read.
	if err := a.SearchSensors(ctx); err != nil {
		t.Fatal(err)
	}
	found, err := a.NewSensors(ctx)
	if err != nil || found.LastScan == "" || found.Found["7"] != "New switch" {
		t.Errorf("new sensors %+v %v", found, err)
	}
	if err := a.SearchLights(ctx, "45AF34"); err != nil {
		t.Fatal(err)
	}
	if s.lastBody()["deviceid"] == nil {
		t.Errorf("light search body %v", s.lastBody())
	}
	nl, err := a.NewLights(ctx)
	if err != nil || nl.Found["7"] != "New lamp" {
		t.Errorf("new lights %+v %v", nl, err)
	}
	if err := a.RenameLight(ctx, "1", "Porch lamp"); err != nil {
		t.Fatal(err)
	}
	if err := a.IdentifyLight(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if st := s.get("lights", "1", "state", "alert"); st != "select" {
		t.Errorf("alert %v", st)
	}
	if err := a.DeleteLight(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSensor(ctx, id); err != nil {
		t.Fatal(err)
	}
	if s.get("lights", "1") != nil || s.get("sensors", id) != nil {
		t.Error("deleted items are still there")
	}
}

func TestV1ConfigAndUpdate(t *testing.T) {
	s, _ := newV1Store(t, "")
	a := hue.NewV1Client(s.addr(), storeKey, nil)
	ctx := context.Background()
	c, err := a.Config(ctx)
	if err != nil || c.Name != "Store bridge" || c.BridgeID != "001788FFFE5A6B7C" || c.ZigbeeChannel != 15 || len(c.Whitelist) != 2 {
		t.Fatalf("config %+v %v", c, err)
	}
	if w := c.Whitelist["other-key"]; w.Name != "other#app" || w.Created == "" || w.LastUsed == "" {
		t.Errorf("whitelist entry %+v", w)
	}
	if err := a.UpdateConfig(ctx, map[string]any{"name": "Hall bridge"}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateConfig(ctx, nil); err == nil {
		t.Error("an empty change was sent")
	}
	if err := a.DeleteUser(ctx, "other-key"); err != nil {
		t.Fatal(err)
	}
	c, _ = a.Config(ctx)
	if c.Name != "Hall bridge" || len(c.Whitelist) != 1 {
		t.Errorf("after the changes %+v", c)
	}
	u, err := a.SoftwareUpdate(ctx)
	if err != nil || u.State != "noupdates" || !u.AutoInstall.On || u.Bridge.LastInstall == "" {
		t.Fatalf("update %+v %v", u, err)
	}
	for _, f := range []func() error{
		func() error { return a.CheckForUpdate(ctx) },
		func() error { return a.InstallUpdate(ctx) },
		func() error { return a.SetAutoInstall(ctx, false, "T04:00:00") },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	if s.get("config", "swupdate2", "checkforupdate") != true || s.get("config", "swupdate2", "install") != true ||
		s.get("config", "swupdate2", "autoinstall", "on") != false || s.get("config", "swupdate2", "autoinstall", "updatetime") != "T04:00:00" {
		t.Errorf("swupdate2 %v", s.get("config", "swupdate2"))
	}
	caps, err := a.Capabilities(ctx)
	if err != nil || caps.Lights.Available != 62 || caps.Sensors.CLIP.Available != 249 || caps.Rules.Conditions.Available != 1500 ||
		caps.Streaming.Channels != 10 || len(caps.Timezones.Values) != 2 || caps.Scenes.LightStates.Available != 2048 {
		t.Errorf("capabilities %+v %v", caps, err)
	}
	st, err := a.FullState(ctx)
	if err != nil || st["rules"] == nil || st["config"] == nil {
		t.Errorf("full state %v %v", st, err)
	}
	// A refused key is ErrUnauthorized, whatever the call.
	if _, err := hue.NewV1Client(s.addr(), "nope", nil).Rules(ctx); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
	// An error answer carries the error type.
	err = a.UpdateRule(ctx, "1", hue.V1Rule{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Request(ctx, "PUT", "/rules/1", map[string]any{"owner": "me"})
	var ae *hue.APIError
	if !errors.As(err, &ae) || !reflect.DeepEqual(ae.Codes, []int{6}) || !strings.Contains(ae.Error(), "owner") {
		t.Errorf("error answer %#v", err)
	}
}

// APIv1 reaches API v1 on a square bridge over the client's HTTPS
// connection, with its certificate check.
func TestAPIv1OverHTTPS(t *testing.T) {
	const id = "001788fffe5a6b7c"
	s, ca := newV1Store(t, id)
	roots := pool(t, ca)
	ctx := context.Background()
	c := hue.NewClient(hue.Config{Address: s.addr(), AppKey: storeKey, TLS: hue.TLSCheck{BridgeID: func() string { return id }, Roots: roots}})
	rules, err := c.APIv1().Rules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules over HTTPS %v %v", rules, err)
	}
	// The certificate check applies: another bridge ID is refused.
	other := hue.NewClient(hue.Config{Address: s.addr(), AppKey: storeKey, TLS: hue.TLSCheck{BridgeID: func() string { return "001788fffe000000" }, Roots: roots}})
	if _, err := other.APIv1().Rules(ctx); err == nil || !strings.Contains(err.Error(), "bridge ID") {
		t.Errorf("wrong bridge ID: %v", err)
	}
	// With V1 set, APIv1 is that client.
	v := hue.NewV1Client("127.0.0.1:1", "k", nil)
	c.V1 = v
	if c.APIv1() != v {
		t.Error("APIv1 is not the V1 client")
	}
}

func TestV1Address(t *testing.T) {
	for in, want := range map[string]string{"v1-light-3": "/lights/3", "v1-light-12-light": "/lights/12", "v1-light-4-zigbee": "/lights/4",
		"v1-group-2": "/groups/2", "v1-group-0-light": "/groups/0", "v1-scene-AbCdEf123": "/scenes/AbCdEf123",
		"v1-sensor-001788010200af28-4": "/sensors/4", "v1-sensor-001788010200af28-power": "", "v1-sensor-001788010200af28": "",
		"0e8a4c1d-0000-4000-8000-000000000001": "", "v1-bridge": ""} {
		got, ok := hue.V1Address(in)
		if got != want && !(want == "" && !ok) {
			t.Errorf("V1Address(%q) = %q %v, want %q", in, got, ok, want)
		}
		if ok != (want != "") {
			t.Errorf("V1Address(%q) ok %v", in, ok)
		}
	}
}
