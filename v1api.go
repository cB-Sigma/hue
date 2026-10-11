// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The parts of API v1 that have no CLIP v2 form: rules, schedules,
// resource links, sensors (including CLIP sensors an application creates
// and sets), the search for new lights and sensors, the bridge
// configuration and its whitelist of application keys, capabilities and
// the software update flags. Round bridges run their automations this
// way; square bridges still answer these requests, over HTTPS (see
// Client.APIv1). Items are addressed by their v1 IDs ("3", not CLIP v2
// IDs); V1Address turns an ID this client made into a v1 address.

// V1Condition is one condition of a rule: an address of the bridge's state
// (such as /sensors/5/state/buttonevent or /config/localtime), an operator
// (eq, gt, lt, dx, ddx, stable, not stable, in, not in) and, for most
// operators, a value written as text.
type V1Condition struct {
	Address  string `json:"address"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
}

// UnmarshalJSON reads a condition whose value is text, a number or a
// boolean (bridges write text; emulators differ).
func (c *V1Condition) UnmarshalJSON(b []byte) error {
	var raw struct {
		Address  string          `json:"address"`
		Operator string          `json:"operator"`
		Value    json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.Address, c.Operator, c.Value = raw.Address, raw.Operator, ""
	if len(raw.Value) > 0 && string(raw.Value) != "null" {
		var s string
		if json.Unmarshal(raw.Value, &s) == nil {
			c.Value = s
		} else {
			c.Value = string(raw.Value)
		}
	}
	return nil
}

// V1Action is a request the bridge makes for a rule or a schedule: an
// address under /api/<key> (such as /groups/1/action), a method (PUT,
// POST or DELETE) and a body.
type V1Action struct {
	Address string         `json:"address"`
	Method  string         `json:"method"`
	Body    map[string]any `json:"body"`
}

// V1Rule is a rule: when every condition holds after a change of the
// state a condition names, the bridge makes the actions' requests.
type V1Rule struct {
	// ID is the rule's v1 ID (not part of the JSON).
	ID             string        `json:"-"`
	Name           string        `json:"name,omitempty"`
	Owner          string        `json:"owner,omitempty"`
	Created        string        `json:"created,omitempty"`
	LastTriggered  string        `json:"lasttriggered,omitempty"`
	TimesTriggered int           `json:"timestriggered,omitempty"`
	Status         string        `json:"status,omitempty"` // enabled, disabled, resourcedeleted
	Recycle        bool          `json:"recycle,omitempty"`
	Conditions     []V1Condition `json:"conditions,omitempty"`
	Actions        []V1Action    `json:"actions,omitempty"`
}

func (r V1Rule) body(create bool) map[string]any {
	b := map[string]any{}
	if r.Name != "" {
		b["name"] = r.Name
	}
	if r.Status != "" {
		b["status"] = r.Status
	}
	if r.Conditions != nil || create {
		b["conditions"] = nonNil(r.Conditions)
	}
	if r.Actions != nil || create {
		b["actions"] = nonNil(r.Actions)
	}
	if create && r.Recycle {
		b["recycle"] = true
	}
	return b
}

// V1Schedule is a timer: at its local time the bridge makes the command's
// request. LocalTime is an absolute time (2026-10-11T07:30:00), a weekly
// time (W124/T07:30:00, the days as a bit mask from Monday = 64 to
// Sunday = 1), a one-off timer (PT00:10:00) or a repeated one
// (R05/PT00:10:00); a random part may follow (A00:05:00).
type V1Schedule struct {
	// ID is the schedule's v1 ID (not part of the JSON).
	ID          string   `json:"-"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Command     V1Action `json:"command"`
	LocalTime   string   `json:"localtime,omitempty"`
	// Time is the UTC form older firmware writes; read only.
	Time      string `json:"time,omitempty"`
	Created   string `json:"created,omitempty"`
	StartTime string `json:"starttime,omitempty"`
	Status    string `json:"status,omitempty"` // enabled, disabled
	// AutoDelete removes a one-off schedule once it ran (the bridge's
	// default is true); nil leaves it to the bridge.
	AutoDelete *bool `json:"autodelete,omitempty"`
	Recycle    bool  `json:"recycle,omitempty"`
}

func (s V1Schedule) body(create bool) map[string]any {
	b := map[string]any{}
	if s.Name != "" {
		b["name"] = s.Name
	}
	if s.Description != "" {
		b["description"] = s.Description
	}
	if s.Command.Address != "" || create {
		cmd := s.Command
		if cmd.Body == nil {
			cmd.Body = map[string]any{}
		}
		b["command"] = cmd
	}
	if s.LocalTime != "" {
		b["localtime"] = s.LocalTime
	}
	if s.Status != "" {
		b["status"] = s.Status
	}
	if s.AutoDelete != nil {
		b["autodelete"] = *s.AutoDelete
	}
	if create && s.Recycle {
		b["recycle"] = true
	}
	return b
}

// V1ResourceLink groups addresses (lights, groups, scenes, rules,
// schedules, sensors) that belong together, such as the parts of one
// application's automation; ClassID is the application's own number.
type V1ResourceLink struct {
	// ID is the link's v1 ID (not part of the JSON).
	ID          string   `json:"-"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty"`
	ClassID     int      `json:"classid,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Recycle     bool     `json:"recycle,omitempty"`
	Links       []string `json:"links,omitempty"`
}

func (l V1ResourceLink) body(create bool) map[string]any {
	b := map[string]any{}
	if l.Name != "" {
		b["name"] = l.Name
	}
	if l.Description != "" || create {
		b["description"] = l.Description
	}
	if create {
		b["classid"] = l.ClassID
		if l.Recycle {
			b["recycle"] = true
		}
	}
	if l.Links != nil || create {
		b["links"] = nonNil(l.Links)
	}
	return b
}

// V1Sensor is a sensor of API v1: a physical one (ZLLPresence, ZLLSwitch,
// ZLLTemperature, …) or a CLIP sensor an application created
// (CLIPGenericStatus, CLIPGenericFlag, CLIPPresence, …), whose state the
// application sets and rules read.
type V1Sensor struct {
	// ID is the sensor's v1 ID (not part of the JSON).
	ID               string         `json:"-"`
	Name             string         `json:"name,omitempty"`
	Type             string         `json:"type,omitempty"`
	ModelID          string         `json:"modelid,omitempty"`
	ManufacturerName string         `json:"manufacturername,omitempty"`
	ProductName      string         `json:"productname,omitempty"`
	SWVersion        string         `json:"swversion,omitempty"`
	UniqueID         string         `json:"uniqueid,omitempty"`
	Recycle          bool           `json:"recycle,omitempty"`
	State            map[string]any `json:"state,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
}

// V1NewDevices is the result of the last search for lights or sensors.
type V1NewDevices struct {
	// LastScan is "active" while a search runs, "none" before the first
	// one, else the time the last one ended.
	LastScan string
	// Found maps the v1 IDs of the devices that search found to their
	// names.
	Found map[string]string
}

// V1WhitelistEntry is an application key the bridge accepts.
type V1WhitelistEntry struct {
	Name     string `json:"name"`
	Created  string `json:"create date"`
	LastUsed string `json:"last use date"`
}

// V1SoftwareUpdate is the bridge's software update state (swupdate2 in
// the configuration).
type V1SoftwareUpdate struct {
	CheckForUpdate bool   `json:"checkforupdate"`
	LastChange     string `json:"lastchange,omitempty"`
	// State is noupdates, transferring, anyreadytoinstall, allreadytoinstall
	// or installing.
	State  string `json:"state,omitempty"`
	Bridge struct {
		State       string `json:"state,omitempty"`
		LastInstall string `json:"lastinstall,omitempty"`
	} `json:"bridge"`
	AutoInstall struct {
		On         bool   `json:"on"`
		UpdateTime string `json:"updatetime,omitempty"`
	} `json:"autoinstall"`
}

// V1BridgeConfig is the bridge's whole configuration (GET /config with a
// key): the public part and the network, time, whitelist and software
// update settings.
type V1BridgeConfig struct {
	V1Config
	MAC              string                      `json:"mac,omitempty"`
	IPAddress        string                      `json:"ipaddress,omitempty"`
	Netmask          string                      `json:"netmask,omitempty"`
	Gateway          string                      `json:"gateway,omitempty"`
	DHCP             bool                        `json:"dhcp,omitempty"`
	ProxyAddress     string                      `json:"proxyaddress,omitempty"`
	ProxyPort        int                         `json:"proxyport,omitempty"`
	UTC              string                      `json:"UTC,omitempty"`
	LocalTime        string                      `json:"localtime,omitempty"`
	TimeZone         string                      `json:"timezone,omitempty"`
	ZigbeeChannel    int                         `json:"zigbeechannel,omitempty"`
	LinkButton       bool                        `json:"linkbutton,omitempty"`
	PortalServices   bool                        `json:"portalservices,omitempty"`
	FactoryNew       bool                        `json:"factorynew,omitempty"`
	DatastoreVersion string                      `json:"datastoreversion,omitempty"`
	Whitelist        map[string]V1WhitelistEntry `json:"whitelist,omitempty"`
	SWUpdate2        *V1SoftwareUpdate           `json:"swupdate2,omitempty"`
}

// V1Capacity is how many items of a kind the bridge can still take
// (Available) and can hold in all (Total, 0 when not told).
type V1Capacity struct {
	Available int `json:"available"`
	Total     int `json:"total,omitempty"`
}

// V1Capabilities is what the bridge can hold (GET /capabilities, API 1.15
// and later).
type V1Capabilities struct {
	Lights  V1Capacity `json:"lights"`
	Sensors struct {
		V1Capacity
		CLIP V1Capacity `json:"clip"`
		ZLL  V1Capacity `json:"zll"`
		ZGP  V1Capacity `json:"zgp"`
	} `json:"sensors"`
	Groups V1Capacity `json:"groups"`
	Scenes struct {
		V1Capacity
		LightStates V1Capacity `json:"lightstates"`
	} `json:"scenes"`
	Schedules V1Capacity `json:"schedules"`
	Rules     struct {
		V1Capacity
		Conditions V1Capacity `json:"conditions"`
		Actions    V1Capacity `json:"actions"`
	} `json:"rules"`
	ResourceLinks V1Capacity `json:"resourcelinks"`
	Streaming     struct {
		V1Capacity
		Channels int `json:"channels"`
	} `json:"streaming"`
	Timezones struct {
		Values []string `json:"values"`
	} `json:"timezones"`
}

func nonNil[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}

// APIv1 is a V1Client for the same bridge over the client's own
// connection: HTTPS with the certificate check, the same key and timeout.
// Square bridges answer API v1 there; use it for rules, schedules,
// resource links, sensors and the configuration, which CLIP v2 does not
// serve. When c.V1 is set (a round bridge), it is returned.
func (c *Client) APIv1() *V1Client {
	if c.V1 != nil {
		return c.V1
	}
	return &V1Client{base: c.base, key: c.key, hc: c.hc}
}

// V1Address is the v1 address (/lights/3, /groups/1, /scenes/<id>,
// /sensors/5) of a resource ID that V1Client made (v1-light-3-light,
// v1-group-1, …); ok is false for other IDs.
func V1Address(id string) (addr string, ok bool) {
	switch {
	case strings.HasPrefix(id, "v1-light-"):
		n, _, _ := strings.Cut(strings.TrimPrefix(id, "v1-light-"), "-")
		return "/lights/" + n, n != ""
	case strings.HasPrefix(id, "v1-group-"):
		n, _, _ := strings.Cut(strings.TrimPrefix(id, "v1-group-"), "-")
		return "/groups/" + n, n != ""
	case strings.HasPrefix(id, "v1-scene-"):
		n := strings.TrimPrefix(id, "v1-scene-")
		return "/scenes/" + n, n != ""
	case strings.HasPrefix(id, "v1-sensor-"):
		rest := strings.TrimPrefix(id, "v1-sensor-")
		_, n, found := strings.Cut(rest, "-")
		if !found || n == "" || n == "power" {
			return "", false
		}
		return "/sensors/" + n, true
	}
	return "", false
}

// getJSON reads a v1 path into out.
func (a *V1Client) getJSON(ctx context.Context, path string, out any) error {
	b, err := a.Request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("the bridge's answer to GET %s is not understood: %w", path, err)
	}
	return nil
}

// change sends a PUT, POST or DELETE whose answer is a list of success
// items; error items are an *APIError.
func (a *V1Client) change(ctx context.Context, method, path string, body any) error {
	defer a.dirty()
	_, err := a.Request(ctx, method, path, body)
	return err
}

// dirty makes the next CLIP-shaped read ask the bridge again.
func (a *V1Client) dirty() {
	a.mu.Lock()
	a.stateAt = time.Time{}
	a.mu.Unlock()
}

// list reads a section of numbered items, sorted by ID, setting each ID.
func list[T any](ctx context.Context, a *V1Client, section string, setID func(*T, string)) ([]T, error) {
	var m map[string]json.RawMessage
	if err := a.getJSON(ctx, "/"+section, &m); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortV1IDs(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		var v T
		if err := json.Unmarshal(m[k], &v); err != nil {
			return nil, fmt.Errorf("%s %s: %w", section, k, err)
		}
		setID(&v, k)
		out = append(out, v)
	}
	return out, nil
}

func sortV1IDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		a, ea := strconv.Atoi(ids[i])
		b, eb := strconv.Atoi(ids[j])
		if ea == nil && eb == nil {
			return a < b
		}
		return ids[i] < ids[j]
	})
}

func one[T any](ctx context.Context, a *V1Client, section, id string) (T, error) {
	var v T
	if id == "" {
		return v, errors.New("no " + section + " ID")
	}
	err := a.getJSON(ctx, "/"+section+"/"+id, &v)
	return v, err
}

func needID(section, id string) error {
	if id == "" || strings.Contains(id, "/") {
		return fmt.Errorf("not a %s ID: %q", section, id)
	}
	return nil
}

// FullState reads the bridge's whole state (GET /api/<key>): lights,
// groups, scenes, rules, schedules, resource links, sensors and the
// configuration, as the bridge answers it.
func (a *V1Client) FullState(ctx context.Context) (map[string]any, error) {
	var m map[string]any
	err := a.getJSON(ctx, "", &m)
	return m, err
}

// Rules lists the rules, by ID.
func (a *V1Client) Rules(ctx context.Context) ([]V1Rule, error) {
	return list(ctx, a, "rules", func(r *V1Rule, id string) { r.ID = id })
}

// Rule reads one rule.
func (a *V1Client) Rule(ctx context.Context, id string) (V1Rule, error) {
	r, err := one[V1Rule](ctx, a, "rules", id)
	r.ID = id
	return r, err
}

// CreateRule creates a rule (name, status, conditions, actions, recycle)
// and returns its ID.
func (a *V1Client) CreateRule(ctx context.Context, r V1Rule) (string, error) {
	if len(r.Conditions) == 0 || len(r.Actions) == 0 {
		return "", errors.New("a rule needs conditions and actions")
	}
	defer a.dirty()
	return a.create(ctx, "/rules", r.body(true))
}

// UpdateRule changes a rule: its name, status, and conditions and actions
// when given (each replaces the whole list).
func (a *V1Client) UpdateRule(ctx context.Context, id string, r V1Rule) error {
	if err := needID("rule", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/rules/"+id, r.body(false))
}

// DeleteRule deletes a rule.
func (a *V1Client) DeleteRule(ctx context.Context, id string) error {
	if err := needID("rule", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/rules/"+id, nil)
}

// Schedules lists the schedules, by ID.
func (a *V1Client) Schedules(ctx context.Context) ([]V1Schedule, error) {
	return list(ctx, a, "schedules", func(s *V1Schedule, id string) { s.ID = id })
}

// Schedule reads one schedule.
func (a *V1Client) Schedule(ctx context.Context, id string) (V1Schedule, error) {
	s, err := one[V1Schedule](ctx, a, "schedules", id)
	s.ID = id
	return s, err
}

// CreateSchedule creates a schedule and returns its ID.
func (a *V1Client) CreateSchedule(ctx context.Context, s V1Schedule) (string, error) {
	if s.Command.Address == "" || s.LocalTime == "" {
		return "", errors.New("a schedule needs a command and a local time")
	}
	defer a.dirty()
	return a.create(ctx, "/schedules", s.body(true))
}

// UpdateSchedule changes the fields set in s (the command when its
// address is set).
func (a *V1Client) UpdateSchedule(ctx context.Context, id string, s V1Schedule) error {
	if err := needID("schedule", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/schedules/"+id, s.body(false))
}

// DeleteSchedule deletes a schedule.
func (a *V1Client) DeleteSchedule(ctx context.Context, id string) error {
	if err := needID("schedule", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/schedules/"+id, nil)
}

// ResourceLinks lists the resource links, by ID.
func (a *V1Client) ResourceLinks(ctx context.Context) ([]V1ResourceLink, error) {
	return list(ctx, a, "resourcelinks", func(l *V1ResourceLink, id string) { l.ID = id })
}

// ResourceLink reads one resource link.
func (a *V1Client) ResourceLink(ctx context.Context, id string) (V1ResourceLink, error) {
	l, err := one[V1ResourceLink](ctx, a, "resourcelinks", id)
	l.ID = id
	return l, err
}

// CreateResourceLink creates a resource link and returns its ID.
func (a *V1Client) CreateResourceLink(ctx context.Context, l V1ResourceLink) (string, error) {
	if l.Name == "" {
		return "", errors.New("a resource link needs a name")
	}
	defer a.dirty()
	return a.create(ctx, "/resourcelinks", l.body(true))
}

// UpdateResourceLink changes a link's name, description and, when given,
// its links (the whole list).
func (a *V1Client) UpdateResourceLink(ctx context.Context, id string, l V1ResourceLink) error {
	if err := needID("resource link", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/resourcelinks/"+id, l.body(false))
}

// DeleteResourceLink deletes a resource link (a bridge also deletes the
// linked items marked recycle).
func (a *V1Client) DeleteResourceLink(ctx context.Context, id string) error {
	if err := needID("resource link", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/resourcelinks/"+id, nil)
}

// Sensors lists the v1 sensors, by ID, as the bridge describes them
// (CLIP sensors included; the CLIP-shaped reads leave those out).
func (a *V1Client) Sensors(ctx context.Context) ([]V1Sensor, error) {
	return list(ctx, a, "sensors", func(s *V1Sensor, id string) { s.ID = id })
}

// Sensor reads one sensor.
func (a *V1Client) Sensor(ctx context.Context, id string) (V1Sensor, error) {
	s, err := one[V1Sensor](ctx, a, "sensors", id)
	s.ID = id
	return s, err
}

// CreateSensor creates a CLIP sensor (Type CLIPGenericStatus,
// CLIPGenericFlag, CLIPPresence, CLIPSwitch, CLIPOpenClose,
// CLIPTemperature, CLIPHumidity, CLIPLightlevel, …; bridges want name,
// type, model ID, software version, manufacturer and a unique ID) and
// returns its ID.
func (a *V1Client) CreateSensor(ctx context.Context, s V1Sensor) (string, error) {
	if s.Name == "" || s.Type == "" {
		return "", errors.New("a sensor needs a name and a type")
	}
	b := map[string]any{"name": s.Name, "type": s.Type, "modelid": s.ModelID, "swversion": s.SWVersion,
		"manufacturername": s.ManufacturerName, "uniqueid": s.UniqueID}
	if s.State != nil {
		b["state"] = s.State
	}
	if s.Config != nil {
		b["config"] = s.Config
	}
	if s.Recycle {
		b["recycle"] = true
	}
	defer a.dirty()
	return a.create(ctx, "/sensors", b)
}

// RenameSensor changes a sensor's name.
func (a *V1Client) RenameSensor(ctx context.Context, id, name string) error {
	if err := needID("sensor", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/sensors/"+id, map[string]any{"name": name})
}

// SetSensorState sets attributes of a CLIP sensor's state (status, flag,
// presence, buttonevent, …); the bridge then runs the rules that read
// them. Physical sensors refuse it.
func (a *V1Client) SetSensorState(ctx context.Context, id string, state map[string]any) error {
	if err := needID("sensor", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/sensors/"+id+"/state", state)
}

// SetSensorConfig changes attributes of a sensor's configuration (on,
// sensitivity, tholddark, ledindication, …).
func (a *V1Client) SetSensorConfig(ctx context.Context, id string, config map[string]any) error {
	if err := needID("sensor", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/sensors/"+id+"/config", config)
}

// DeleteSensor deletes a sensor.
func (a *V1Client) DeleteSensor(ctx context.Context, id string) error {
	if err := needID("sensor", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/sensors/"+id, nil)
}

// searchBody is the body of a search: empty, or the serial numbers of
// devices to look for.
func searchBody(deviceIDs []string) any {
	if len(deviceIDs) == 0 {
		return map[string]any{}
	}
	return map[string]any{"deviceid": deviceIDs}
}

// SearchSensors starts the bridge's search for new sensors (about 40 s);
// deviceIDs are serial numbers of devices to look for in particular.
// Read the result with NewSensors.
func (a *V1Client) SearchSensors(ctx context.Context, deviceIDs ...string) error {
	return a.change(ctx, http.MethodPost, "/sensors", searchBody(deviceIDs))
}

// NewSensors reads what the last sensor search found.
func (a *V1Client) NewSensors(ctx context.Context) (V1NewDevices, error) {
	return a.newDevices(ctx, "/sensors/new")
}

// SearchLights starts the bridge's search for new lights (about 40 s);
// deviceIDs are serial numbers of lights to look for in particular.
// Read the result with NewLights.
func (a *V1Client) SearchLights(ctx context.Context, deviceIDs ...string) error {
	return a.change(ctx, http.MethodPost, "/lights", searchBody(deviceIDs))
}

// NewLights reads what the last light search found.
func (a *V1Client) NewLights(ctx context.Context) (V1NewDevices, error) {
	return a.newDevices(ctx, "/lights/new")
}

func (a *V1Client) newDevices(ctx context.Context, path string) (V1NewDevices, error) {
	var m map[string]json.RawMessage
	out := V1NewDevices{Found: map[string]string{}}
	if err := a.getJSON(ctx, path, &m); err != nil {
		return out, err
	}
	for k, v := range m {
		if k == "lastscan" {
			_ = json.Unmarshal(v, &out.LastScan)
			continue
		}
		var d struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(v, &d)
		out.Found[k] = d.Name
	}
	return out, nil
}

// RenameLight changes a light's name (PUT /lights/<id>).
func (a *V1Client) RenameLight(ctx context.Context, id, name string) error {
	if err := needID("light", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/lights/"+id, map[string]any{"name": name})
}

// IdentifyLight makes a light blink once (alert "select").
func (a *V1Client) IdentifyLight(ctx context.Context, id string) error {
	if err := needID("light", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodPut, "/lights/"+id+"/state", map[string]any{"alert": "select"})
}

// DeleteLight removes a light from the bridge (and from its groups and
// scenes).
func (a *V1Client) DeleteLight(ctx context.Context, id string) error {
	if err := needID("light", id); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/lights/"+id, nil)
}

// Config reads the bridge's whole configuration (with a key; ReadConfig
// reads the public part without one).
func (a *V1Client) Config(ctx context.Context) (V1BridgeConfig, error) {
	var c V1BridgeConfig
	err := a.getJSON(ctx, "/config", &c)
	return c, err
}

// UpdateConfig changes configuration attributes (name, zigbeechannel,
// timezone, proxyaddress, linkbutton, swupdate2, …), as the bridge allows.
func (a *V1Client) UpdateConfig(ctx context.Context, changes map[string]any) error {
	if len(changes) == 0 {
		return errors.New("no configuration change")
	}
	return a.change(ctx, http.MethodPut, "/config", changes)
}

// DeleteUser removes an application key from the bridge's whitelist; the
// bridge refuses that key from then on.
func (a *V1Client) DeleteUser(ctx context.Context, key string) error {
	if err := needID("application key", key); err != nil {
		return err
	}
	return a.change(ctx, http.MethodDelete, "/config/whitelist/"+key, nil)
}

// Capabilities reads how many lights, sensors, groups, scenes, rules,
// schedules, resource links and streams the bridge can still take, and its
// time zones.
func (a *V1Client) Capabilities(ctx context.Context) (V1Capabilities, error) {
	var c V1Capabilities
	err := a.getJSON(ctx, "/capabilities", &c)
	return c, err
}

// SoftwareUpdate reads the bridge's software update state.
func (a *V1Client) SoftwareUpdate(ctx context.Context) (V1SoftwareUpdate, error) {
	c, err := a.Config(ctx)
	if err != nil {
		return V1SoftwareUpdate{}, err
	}
	if c.SWUpdate2 == nil {
		return V1SoftwareUpdate{}, errors.New("the bridge's configuration has no swupdate2 (firmware before API 1.20)")
	}
	return *c.SWUpdate2, nil
}

// CheckForUpdate asks the bridge to look for new software for itself and
// its devices (swupdate2.checkforupdate); SoftwareUpdate shows the
// result.
func (a *V1Client) CheckForUpdate(ctx context.Context) error {
	return a.UpdateConfig(ctx, map[string]any{"swupdate2": map[string]any{"checkforupdate": true}})
}

// InstallUpdate asks the bridge to install the software that is ready
// (swupdate2.install); the bridge and its devices restart while it runs.
func (a *V1Client) InstallUpdate(ctx context.Context) error {
	return a.UpdateConfig(ctx, map[string]any{"swupdate2": map[string]any{"install": true}})
}

// SetAutoInstall turns automatic software updates on or off, at a time of
// day such as "T03:00:00" (empty leaves the time).
func (a *V1Client) SetAutoInstall(ctx context.Context, on bool, updateTime string) error {
	ai := map[string]any{"on": on}
	if updateTime != "" {
		ai["updatetime"] = updateTime
	}
	return a.UpdateConfig(ctx, map[string]any{"swupdate2": map[string]any{"autoinstall": ai}})
}
