// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"encoding/json"
	"testing"

	"github.com/cB-Sigma/hue"
)

// res parses a resource written as JSON (made-up demo values).
func res(t *testing.T, s string) hue.Resource {
	t.Helper()
	var r hue.Resource
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTypedSensorViews(t *testing.T) {
	owner := `"owner": {"rid": "dev-9", "rtype": "device"}`
	m, err := hue.As[hue.Motion](res(t, `{"id": "m1", "type": "motion", `+owner+`, "enabled": true,
		"motion": {"motion": false, "motion_valid": true, "motion_report": {"changed": "2026-10-11T10:00:00.120Z", "motion": true}},
		"sensitivity": {"sensitivity": 2, "sensitivity_max": 4, "status": "set"}}`))
	if err != nil || !m.Enabled || m.Owner.RID != "dev-9" || m.Sensitivity.SensitivityMax != 4 {
		t.Fatalf("motion %+v %v", m, err)
	}
	if v, ok := m.Motion.Value(); !v || !ok {
		t.Errorf("the report wins over the old field: %v %v", v, ok)
	}
	old, _ := hue.As[hue.Motion](res(t, `{"id": "m2", "type": "motion", "motion": {"motion": true, "motion_valid": false}}`))
	if _, ok := old.Motion.Value(); ok {
		t.Error("an invalid reading counts")
	}
	var none *hue.MotionFeature
	if _, ok := none.Value(); ok {
		t.Error("no feature has a value")
	}

	tm, err := hue.As[hue.Temperature](res(t, `{"id": "t1", "type": "temperature", "enabled": true, "temperature": {"temperature": 21.5, "temperature_valid": true}}`))
	if v, ok := tm.Value(); err != nil || !ok || v != 21.5 {
		t.Errorf("temperature %v %v %v", v, ok, err)
	}
	tm, _ = hue.As[hue.Temperature](res(t, `{"id": "t1", "type": "temperature", "temperature": {"temperature_report": {"changed": "x", "temperature": 19.25}}}`))
	if v, _ := tm.Value(); v != 19.25 {
		t.Errorf("temperature report %v", v)
	}
	ll, err := hue.As[hue.LightLevel](res(t, `{"id": "l1", "type": "light_level", "enabled": true, "light": {"light_level": 23980, "light_level_valid": true}}`))
	if v, ok := ll.Light.Value(); err != nil || !ok || v != 23980 || hue.LuxFromLightLevel(float64(v)) < 249 {
		t.Errorf("light level %v %v %v", v, ok, err)
	}
	gl, err := hue.As[hue.GroupedLightLevel](res(t, `{"id": "g1", "type": "grouped_light_level", "owner": {"rid": "room-1", "rtype": "room"}, "enabled": true, "light": {"light_level_report": {"changed": "x", "light_level": 100}}}`))
	if v, _ := gl.Light.Value(); err != nil || v != 100 || gl.Owner.RType != "room" {
		t.Errorf("grouped light level %+v %v", gl, err)
	}
	gm, err := hue.As[hue.GroupedMotion](res(t, `{"id": "g2", "type": "grouped_motion", "enabled": true, "motion": {"motion_report": {"changed": "x", "motion": false}}}`))
	if v, ok := gm.Motion.Value(); err != nil || v || !ok {
		t.Errorf("grouped motion %+v %v", gm, err)
	}

	b, err := hue.As[hue.Button](res(t, `{"id": "b1", "type": "button", `+owner+`, "metadata": {"control_id": 3},
		"button": {"last_event": "short_release", "button_report": {"updated": "x", "event": "long_press"}, "repeat_interval": 800, "event_values": ["initial_press", "long_press"]}}`))
	if err != nil || b.Metadata.ControlID != 3 || b.Event() != "long_press" || *b.Button.RepeatInterval != 800 || len(b.Button.EventValues) != 2 {
		t.Errorf("button %+v %v", b, err)
	}
	if (hue.Button{}).Event() != "" {
		t.Error("a button without a feature has an event")
	}
	rr, err := hue.As[hue.RelativeRotary](res(t, `{"id": "r1", "type": "relative_rotary", "relative_rotary": {"last_event": {"action": "start", "rotation": {"direction": "clock_wise", "steps": 30, "duration": 400}},
		"rotary_report": {"updated": "x", "action": "repeat", "rotation": {"direction": "counter_clock_wise", "steps": 75, "duration": 200}}}}`))
	if ev, ok := rr.Event(); err != nil || !ok || ev.Action != "repeat" || ev.Rotation.Steps != 75 || ev.Rotation.Direction != "counter_clock_wise" {
		t.Errorf("rotary %+v %v", ev, err)
	}

	c, err := hue.As[hue.Contact](res(t, `{"id": "c1", "type": "contact", "enabled": true, "contact_report": {"changed": "x", "state": "no_contact"}}`))
	if open, ok := c.Open(); err != nil || !open || !ok {
		t.Errorf("contact %+v %v", c, err)
	}
	tp, err := hue.As[hue.Tamper](res(t, `{"id": "tp1", "type": "tamper", "tamper_reports": [{"changed": "x", "source": "battery_door", "state": "tampered"}]}`))
	if err != nil || len(tp.TamperReports) != 1 || tp.TamperReports[0].State != "tampered" {
		t.Errorf("tamper %+v %v", tp, err)
	}
	dp, err := hue.As[hue.DevicePower](res(t, `{"id": "p1", "type": "device_power", "power_state": {"battery_state": "low", "battery_level": 12}}`))
	if err != nil || dp.PowerState.BatteryState != "low" || *dp.PowerState.BatteryLevel != 12 {
		t.Errorf("power %+v %v", dp, err)
	}
	zc, err := hue.As[hue.ZigbeeConnectivity](res(t, `{"id": "z1", "type": "zigbee_connectivity", "status": "connectivity_issue", "mac_address": "00:17:88:01:0b:aa:bb:cc-0b",
		"channel": {"value": "channel_25", "status": "set"}, "extended_pan_id": "0a1b2c3d4e5f6071"}`))
	if err != nil || zc.Status != "connectivity_issue" || zc.Channel.Value != "channel_25" || zc.ExtendedPanID == "" {
		t.Errorf("zigbee %+v %v", zc, err)
	}
	zg, err := hue.As[hue.ZGPConnectivity](res(t, `{"id": "zg1", "type": "zgp_connectivity", "status": "connected", "source_id": "0012abcd"}`))
	if err != nil || zg.SourceID != "0012abcd" {
		t.Errorf("zgp %+v %v", zg, err)
	}
}

func TestTypedBridgeAndAutomationViews(t *testing.T) {
	br, err := hue.As[hue.Bridge](res(t, `{"id": "br1", "type": "bridge", "bridge_id": "001788fffe4a1b2c", "time_zone": {"time_zone": "Asia/Kolkata"}, "owner": {"rid": "d", "rtype": "device"}}`))
	if err != nil || br.BridgeID != "001788fffe4a1b2c" || br.TimeZone.TimeZone != "Asia/Kolkata" {
		t.Errorf("bridge %+v %v", br, err)
	}
	h, err := hue.As[hue.BridgeHome](res(t, `{"id": "h1", "type": "bridge_home", "children": [{"rid": "room-1", "rtype": "room"}], "services": [{"rid": "gl0", "rtype": "grouped_light"}]}`))
	if err != nil || len(h.Children) != 1 || h.Services[0].RType != "grouped_light" {
		t.Errorf("home %+v %v", h, err)
	}
	ss, err := hue.As[hue.SmartScene](res(t, `{"id": "s1", "type": "smart_scene", "metadata": {"name": "Natural light"}, "group": {"rid": "room-1", "rtype": "room"},
		"week_timeslots": [{"timeslots": [{"start_time": {"kind": "time", "time": {"hour": 7, "minute": 0, "second": 0}}, "target": {"rid": "sc1", "rtype": "scene"}},
			{"start_time": {"kind": "sunset"}, "target": {"rid": "sc2", "rtype": "scene"}}], "recurrence": ["monday", "tuesday"]}],
		"transition_duration": 60000, "state": "active", "active_timeslot": {"timeslot_id": 1, "weekday": "monday"}}`))
	if err != nil || ss.Group.RID != "room-1" || len(ss.WeekTimeslots) != 1 || ss.WeekTimeslots[0].Timeslots[0].StartTime.Time.Hour != 7 ||
		ss.WeekTimeslots[0].Timeslots[1].StartTime.Kind != "sunset" || ss.State != "active" || ss.ActiveTimeslot.Weekday != "monday" {
		t.Errorf("smart scene %+v %v", ss, err)
	}
	bs, err := hue.As[hue.BehaviorScript](res(t, `{"id": "bs1", "type": "behavior_script", "description": "Wake up", "configuration_schema": {"$ref": "x"}, "trigger_schema": {}, "state_schema": {},
		"version": "0.0.1", "metadata": {"name": "Wake up", "category": "automation"}, "supported_features": ["style_sunrise"], "max_number_instances": 25}`))
	if err != nil || bs.Metadata.Category != "automation" || *bs.MaxNumberInstances != 25 || bs.ConfigurationSchema["$ref"] != "x" {
		t.Errorf("script %+v %v", bs, err)
	}
	es, err := hue.As[hue.EntertainmentService](res(t, `{"id": "e1", "type": "entertainment", "renderer": true, "renderer_reference": {"rid": "l1", "rtype": "light"}, "proxy": false,
		"equalizer": true, "max_streams": 1, "segments": {"configurable": false, "max_segments": 7, "segments": [{"start": 0, "length": 1}]}}`))
	if err != nil || !es.Renderer || es.RendererReference.RID != "l1" || es.Segments.MaxSegments != 7 {
		t.Errorf("entertainment %+v %v", es, err)
	}
	su, err := hue.As[hue.DeviceSoftwareUpdate](res(t, `{"id": "u1", "type": "device_software_update", "state": "ready_to_install", "problems": []}`))
	if err != nil || su.State != "ready_to_install" {
		t.Errorf("update %+v %v", su, err)
	}
	g, err := hue.As[hue.Geolocation](res(t, `{"id": "g1", "type": "geolocation", "is_configured": true, "sun_today": {"sunset_time": "18:02:00", "day_type": "normal_day"}}`))
	if err != nil || !g.IsConfigured || g.SunToday.SunsetTime != "18:02:00" {
		t.Errorf("geolocation %+v %v", g, err)
	}
	hk, err := hue.As[hue.Homekit](res(t, `{"id": "hk", "type": "homekit", "status": "unpaired"}`))
	mt, err2 := hue.As[hue.Matter](res(t, `{"id": "mt", "type": "matter", "max_fabrics": 16, "has_qr_code": true}`))
	gc, err3 := hue.As[hue.GeofenceClient](res(t, `{"id": "gc", "type": "geofence_client", "name": "Phone"}`))
	if err != nil || err2 != nil || err3 != nil || hk.Status != "unpaired" || mt.MaxFabrics != 16 || !mt.HasQRCode || gc.Name != "Phone" {
		t.Errorf("homekit %+v matter %+v geofence %+v", hk, mt, gc)
	}
	l, err := hue.As[hue.Light](res(t, `{"id": "l1", "type": "light", "on": {"on": true}, "alert": {"action_values": ["breathe"]},
		"effects": {"status": "candle", "effect_values": ["no_effect", "candle"]}, "gradient": {"points": [{"color": {"xy": {"x": 0.3, "y": 0.4}}}], "points_capable": 5},
		"powerup": {"preset": "safety"}}`))
	if err != nil || l.Alert.ActionValues[0] != "breathe" || l.Effects.Status != "candle" || l.Gradient.Points[0].Color.XY.Y != 0.4 || l.Powerup["preset"] != "safety" {
		t.Errorf("light %+v %v", l, err)
	}
}
