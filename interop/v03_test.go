// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cB-Sigma/hue"
	"github.com/cB-Sigma/hue/discover"
)

// The v0.3.0 additions against diyHue: API v1 rules (run by diyHue on a
// CLIP sensor change), schedules (a timer diyHue runs), resource links,
// CLIP sensors, the configuration and its whitelist, capabilities, the
// light search, API v1 over HTTPS, and SSDP discovery against diyHue's
// SSDP responder. diyHue's software update is read only: asking it to
// check or install would make it reach GitHub.

// lampNumber is the v1 number of the dummy light.
func lampNumber(t *testing.T, a *hue.V1Client) string {
	t.Helper()
	raw, err := a.Request(context.Background(), http.MethodGet, "/lights", nil)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &all)
	for n, l := range all {
		if l.Name == "Interop lamp" {
			return n
		}
	}
	t.Fatalf("no lamp among %v", all)
	return ""
}

func lampOn(t *testing.T, a *hue.V1Client, n string) bool {
	t.Helper()
	raw, err := a.Request(context.Background(), http.MethodGet, "/lights/"+n, nil)
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		State struct {
			On bool `json:"on"`
		} `json:"state"`
	}
	_ = json.Unmarshal(raw, &l)
	return l.State.On
}

func waitFor(cond func() bool, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return cond()
}

func TestV1RulesSensorsLinks(t *testing.T) {
	p := need(t)
	c := client(t, p)
	_ = lamp(t, c)
	ctx := context.Background()
	a := hue.NewV1Client(p.httpAddr, p.pair.AppKey, nil)
	n := lampNumber(t, a)
	if _, err := a.Request(ctx, http.MethodPut, "/lights/"+n+"/state", map[string]any{"on": false}); err != nil {
		t.Fatal(err)
	}
	// diyHue answers every POST /sensors with its device search (seen on
	// 2026-10-12, restful.py of commit 3ee0461), so it cannot create a CLIP
	// sensor: the client reports that the answer names no new sensor.
	if _, err := a.CreateSensor(ctx, hue.V1Sensor{Name: "Interop status", Type: "CLIPGenericStatus", ModelID: "SIGMASTATUS", SWVersion: "1.0",
		ManufacturerName: "Sigma", UniqueID: "sigma-interop-status-1", State: map[string]any{"status": 0}}); errors.Is(err, hue.ErrNoReference) {
		t.Logf("CLIP sensor not created: diyHue answered with its device search (%v)", err)
	} else if err != nil {
		t.Errorf("create sensor: %v", err)
	}
	sensors, err := a.Sensors(ctx)
	if err != nil || len(sensors) == 0 {
		t.Fatalf("sensors %v %v", sensors, err)
	}
	sensor := sensors[0].ID
	t.Logf("%d sensors; using %s (%s, %s)", len(sensors), sensor, sensors[0].Name, sensors[0].Type)
	// A rule diyHue runs: when the zone's lights are switched (dx on its
	// any_on), set the lamp's brightness.
	zone, err := a.Request(ctx, http.MethodPost, "/groups", map[string]any{"name": "Interop rule zone", "type": "Zone", "lights": []any{n}})
	if err != nil {
		t.Fatal(err)
	}
	var made []struct {
		Success struct {
			ID string `json:"id"`
		} `json:"success"`
	}
	_ = json.Unmarshal(zone, &made)
	if len(made) != 1 || made[0].Success.ID == "" {
		t.Fatalf("zone %s", zone)
	}
	g := made[0].Success.ID
	defer func() { _, _ = a.Request(ctx, http.MethodDelete, "/groups/"+g, nil) }()
	rule, err := a.CreateRule(ctx, hue.V1Rule{Name: "Interop rule",
		Conditions: []hue.V1Condition{{Address: "/groups/" + g + "/state/any_on", Operator: "dx"}},
		Actions:    []hue.V1Action{{Address: "/lights/" + n + "/state", Method: "PUT", Body: map[string]any{"bri": 77}}}})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := a.Request(ctx, http.MethodPut, "/groups/"+g+"/action", map[string]any{"on": true}); err != nil {
		t.Fatal(err)
	}
	bri := func() float64 {
		raw, _ := a.Request(ctx, http.MethodGet, "/lights/"+n, nil)
		var l struct {
			State struct {
				Bri float64 `json:"bri"`
			} `json:"state"`
		}
		_ = json.Unmarshal(raw, &l)
		return l.State.Bri
	}
	if !waitFor(func() bool { return bri() == 77 }, 10*time.Second) {
		logs, _ := docker("logs", "--tail", "30", the.name)
		t.Errorf("diyHue did not run the rule (brightness %v)\n%s", bri(), logs)
	}
	r, err := a.Rule(ctx, rule)
	if err != nil || r.Name != "Interop rule" || len(r.Conditions) != 1 || r.Conditions[0].Operator != "dx" || r.Actions[0].Body["bri"] != 77.0 {
		t.Errorf("rule %+v %v", r, err)
	}
	t.Logf("rule as diyHue keeps it: owner %q, status %q, triggered %d times, last %q", r.Owner, r.Status, r.TimesTriggered, r.LastTriggered)
	if err := a.UpdateRule(ctx, rule, hue.V1Rule{Status: "disabled"}); err != nil {
		t.Error(err)
	} else if r, _ := a.Rule(ctx, rule); r.Status != "disabled" {
		t.Errorf("status after update %q", r.Status)
	}
	rules, err := a.Rules(ctx)
	if err != nil || len(rules) == 0 {
		t.Errorf("rules %v %v", rules, err)
	}
	// The same through APIv1 over HTTPS, with the certificate check.
	if over, err := c.APIv1().Rules(ctx); err != nil || len(over) != len(rules) {
		t.Errorf("rules over HTTPS: %d %v", len(over), err)
	}
	if err := a.SetSensorConfig(ctx, sensor, map[string]any{"on": true}); err != nil {
		t.Errorf("sensor config: %v", err)
	}
	if err := a.RenameSensor(ctx, sensor, "Interop sensor"); err != nil {
		t.Error(err)
	}
	s, err := a.Sensor(ctx, sensor)
	if err != nil || s.Name != "Interop sensor" || s.Type != sensors[0].Type {
		t.Errorf("sensor %+v %v", s, err)
	}
	_ = a.RenameSensor(ctx, sensor, sensors[0].Name)
	link, err := a.CreateResourceLink(ctx, hue.V1ResourceLink{Name: "Interop link", Description: "the interop test's items", ClassID: 10010,
		Links: []string{"/sensors/" + sensor, "/rules/" + rule}})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	if err := a.UpdateResourceLink(ctx, link, hue.V1ResourceLink{Links: []string{"/rules/" + rule}}); err != nil {
		t.Error(err)
	}
	l, err := a.ResourceLink(ctx, link)
	if err != nil || l.ClassID != 10010 || len(l.Links) != 1 || l.Type != "Link" {
		t.Errorf("link %+v %v", l, err)
	}
	for what, f := range map[string]func() error{
		"link": func() error { return a.DeleteResourceLink(ctx, link) },
		"rule": func() error { return a.DeleteRule(ctx, rule) },
	} {
		if err := f(); err != nil {
			t.Errorf("delete %s: %v", what, err)
		}
	}
	if _, err := a.Rule(ctx, rule); err == nil {
		t.Error("the rule is still there")
	} else {
		var ae *hue.APIError
		if errors.As(err, &ae) {
			t.Logf("deleted rule: error types %v", ae.Codes)
		}
	}
}

func TestV1Schedule(t *testing.T) {
	p := need(t)
	c := client(t, p)
	_ = lamp(t, c)
	ctx := context.Background()
	a := hue.NewV1Client(p.httpAddr, p.pair.AppKey, nil)
	n := lampNumber(t, a)
	// diyHue compares its timers to the clock once a second and can step
	// over the second; the timer is tried up to three times.
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := a.Request(ctx, http.MethodPut, "/lights/"+n+"/state", map[string]any{"on": true}); err != nil {
			t.Fatal(err)
		}
		id, err := a.CreateSchedule(ctx, hue.V1Schedule{Name: "Interop timer", LocalTime: "PT00:00:03", Status: "enabled",
			Command: hue.V1Action{Address: "/api/" + p.pair.AppKey + "/lights/" + n + "/state", Method: "PUT", Body: map[string]any{"on": false}}})
		if err != nil {
			t.Fatalf("create schedule: %v", err)
		}
		s, err := a.Schedule(ctx, id)
		if err != nil || s.LocalTime != "PT00:00:03" || s.Command.Method != "PUT" {
			t.Errorf("schedule %+v %v", s, err)
		}
		ran := waitFor(func() bool { return !lampOn(t, a, n) }, 10*time.Second)
		if err := a.UpdateSchedule(ctx, id, hue.V1Schedule{Name: "Interop timer (done)"}); err != nil {
			t.Errorf("update: %v", err)
		}
		if err := a.DeleteSchedule(ctx, id); err != nil {
			t.Errorf("delete: %v", err)
		}
		if ran {
			t.Logf("attempt %d: diyHue ran the timer", attempt)
			return
		}
		t.Logf("attempt %d: the timer did not run within 10 s", attempt)
	}
	t.Error("diyHue did not run the timer in three attempts")
}

func TestV1ConfigWhitelistCapabilities(t *testing.T) {
	p := need(t)
	ctx := context.Background()
	a := hue.NewV1Client(p.httpAddr, p.pair.AppKey, nil)
	cfg, err := a.Config(ctx)
	if err != nil || hue.NormaliseBridgeID(cfg.BridgeID) != bridgeID || cfg.Whitelist[p.pair.AppKey].Name != "sigma-interop#test" {
		t.Fatalf("config %+v %v", cfg, err)
	}
	if cfg.SWUpdate2 != nil {
		t.Logf("software update state %q, bridge %q", cfg.SWUpdate2.State, cfg.SWUpdate2.Bridge.State)
	}
	if u, err := a.SoftwareUpdate(ctx); err != nil {
		t.Errorf("software update: %v", err)
	} else {
		t.Logf("swupdate2 read: %+v", u)
	}
	old := cfg.Name
	if err := a.UpdateConfig(ctx, map[string]any{"name": "Interop bridge"}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := a.Config(ctx); cfg.Name != "Interop bridge" {
		t.Errorf("name %q", cfg.Name)
	}
	_ = a.UpdateConfig(ctx, map[string]any{"name": old})
	// A second key, removed from the whitelist and then refused.
	if err := pressLinkButton(p.name); err != nil {
		t.Fatal(err)
	}
	pc := hue.NewClient(hue.Config{Address: p.httpsAddr, TLS: check(t, p), Timeout: 5 * time.Second})
	second, err := pc.Pair(ctx, "sigma-interop#second")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteUser(ctx, second.AppKey); err != nil {
		t.Fatal(err)
	}
	if _, err := hue.NewV1Client(p.httpAddr, second.AppKey, nil).Rules(ctx); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("removed key: %v", err)
	}
	caps, err := a.Capabilities(ctx)
	if err != nil || caps.Lights.Available == 0 {
		t.Errorf("capabilities %+v %v", caps, err)
	}
	t.Logf("capabilities: lights %d, sensors %d, rules %d, schedules %d, %d time zones", caps.Lights.Available, caps.Sensors.Available,
		caps.Rules.Available, caps.Schedules.Available, len(caps.Timezones.Values))
	// The light search: diyHue scans its network in the background.
	if err := a.SearchLights(ctx); err != nil {
		t.Fatal(err)
	}
	nl, err := a.NewLights(ctx)
	if err != nil {
		t.Errorf("new lights: %v", err)
	}
	t.Logf("after the search: lastscan %q, %d found", nl.LastScan, len(nl.Found))
}

func TestSSDPAgainstDiyHue(t *testing.T) {
	p := need(t)
	var found []discover.Bridge
	var err error
	// diyHue answers after a random pause of up to a second.
	for i := 0; i < 3 && len(found) == 0; i++ {
		found, err = discover.SSDP(context.Background(), discover.SSDPOptions{Addr: p.ssdpAddr, Wait: 3 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(found) != 1 || found[0].ID != bridgeID || found[0].Location == "" {
		t.Fatalf("found %+v", found)
	}
	t.Logf("SSDP: %s at %s (location %s)", found[0].ID, found[0].Host, found[0].Location)
}
