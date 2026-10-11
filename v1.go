package hue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Hue API v1 is all the first (round) bridge speaks, and what bridges
// before CLIP v2 firmware speak: plain HTTP on port 80, the application
// key in the path (/api/<key>/…), the whole state in one GET /api/<key>
// (lights, groups, sensors, scenes, config), and PUTs to
// lights/<n>/state and groups/<n>/action. Answers to changes are lists of
// {"success": …} or {"error": {type, address, description}}; error type 1
// is an unauthorised key. Philips documents it on its developer site.
//
// A caller can keep one model of a bridge, the CLIP v2 one: V1Client turns
// the v1 state into v2-shaped resources (devices with light, motion,
// temperature, light_level, button, device_power and zigbee_connectivity
// services; rooms and zones with grouped_light; scenes; the bridge) and v2
// changes back into v1 requests, so a cache, a mapping and commands
// written for CLIP v2 serve both. A v1 bridge has no event stream: it is
// polled, and button presses and motion are told from changes between
// polls (TakeEvents).

// v1 resource IDs: v1 numbers behind a prefix, so they never meet v2 IDs.
func v1LightDev(n string) string   { return "v1-light-" + n }
func v1LightSvc(n string) string   { return "v1-light-" + n + "-light" }
func v1LightConn(n string) string  { return "v1-light-" + n + "-zigbee" }
func v1Group(n string) string      { return "v1-group-" + n }
func v1GroupLight(n string) string { return "v1-group-" + n + "-light" }
func v1Scene(id string) string     { return "v1-scene-" + id }
func v1SensorDev(mac string) string {
	return "v1-sensor-" + strings.NewReplacer(":", "", "-", "").Replace(strings.ToLower(mac))
}

const v1BridgeDev, v1BridgeSvc = "v1-bridge", "v1-bridge-service"

// V1Client speaks API v1 to one bridge. Set it as a Client's V1 to read
// and change the bridge with CLIP v2 requests.
type V1Client struct {
	base string // http://host[:port]
	key  string
	hc   *http.Client

	mu       sync.Mutex
	state    map[string]any // the last GET /api/<key>
	stateAt  time.Time
	res      map[string][]json.RawMessage // v2 resources by type, from state
	lightNum map[string]string            // v2 light service ID → v1 light number
	groupNum map[string]string            // v2 grouped_light ID → v1 group number
	sceneOf  map[string][2]string         // v2 scene ID → v1 scene ID, v1 group number
	lastBtn  map[string]string            // sensor number → lastupdated seen
	lastMot  map[string]bool              // sensor number → presence seen
	pending  []Event                      // presses and motion found by the last reads
}

// NewV1Client returns a client for the v1 bridge at address (IP, host,
// host:port or URL; plain HTTP) with an application key; hc carries the
// requests and their timeout.
func NewV1Client(address, key string, hc *http.Client) *V1Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &V1Client{base: V1Base(address), key: key, hc: hc}
}

// TakeEvents hands out the presses and motion starts the reads found
// (as CLIP v2 update events); take them after applying the state the
// reads returned, which already holds the motion.
func (a *V1Client) TakeEvents() []Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	evs := a.pending
	a.pending = nil
	return evs
}

// v1Archetypes maps v1 light archetypes (config.archetype) to v2 ones.
var v1Archetypes = map[string]string{"classicbulb": "classic_bulb", "sultanbulb": "sultan_bulb", "floodbulb": "flood_bulb",
	"spotbulb": "spot_bulb", "candlebulb": "candle_bulb", "vintagebulb": "vintage_bulb", "huelightstrip": "hue_lightstrip",
	"huego": "hue_go", "hueiris": "hue_iris", "huebloom": "hue_bloom", "hueplay": "hue_play", "pendantround": "pendant_round",
	"ceilinground": "ceiling_round", "tableshade": "table_shade", "floorshade": "floor_shade", "plug": "plug", "huesigne": "hue_signe"}

// archetypeOfV1 tells a v1 light's archetype: its config, else its type.
func archetypeOfV1(l map[string]any) string {
	cfg, _ := l["config"].(map[string]any)
	if a, _ := cfg["archetype"].(string); a != "" {
		if v2, ok := v1Archetypes[strings.ToLower(a)]; ok {
			return v2
		}
		return strings.ToLower(a)
	}
	typ := strings.ToLower(str(l["type"]))
	switch {
	case strings.Contains(typ, "plug") || strings.Contains(typ, "on/off"):
		return "plug"
	case strings.Contains(strings.ToLower(str(l["productname"])), "lightstrip"):
		return "hue_lightstrip"
	}
	return "classic_bulb"
}

func str(v any) string { s, _ := v.(string); return s }

func num(v any) (float64, bool) { f, ok := v.(float64); return f, ok }

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, ea := strconv.Atoi(out[i])
		b, eb := strconv.Atoi(out[j])
		if ea == nil && eb == nil {
			return a < b
		}
		return out[i] < out[j]
	})
	return out
}

// v1Error is an error item of a v1 answer.
type v1Error struct {
	Type        int    `json:"type"`
	Address     string `json:"address"`
	Description string `json:"description"`
}

// Request sends one v1 request (path after /api/<key>, body marshalled
// as JSON when not nil) and returns the answer. Error type 1 is
// ErrUnauthorized; other error items are an *APIError, returned with the
// answer. The key is never in an error's text.
func (a *V1Client) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+"/api/"+a.key+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, Scrub(err, a.key)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode}
	}
	// An error answer is a list; an unauthorised key is error type 1.
	var items []struct {
		Error *v1Error `json:"error"`
	}
	if json.Unmarshal(b, &items) == nil {
		e := &APIError{Status: resp.StatusCode}
		for _, it := range items {
			if it.Error == nil {
				continue
			}
			if it.Error.Type == 1 {
				return nil, fmt.Errorf("%w (API v1 error 1)", ErrUnauthorized)
			}
			e.Desc = append(e.Desc, strings.ReplaceAll(it.Error.Description, a.key, "…"))
		}
		if len(e.Desc) > 0 {
			return b, e
		}
	}
	return b, nil
}

// Refresh reads the whole state and rebuilds the v2 resources; within a
// second the last read is reused (a full read asks once per type).
func (a *V1Client) Refresh(ctx context.Context) error {
	a.mu.Lock()
	fresh := time.Since(a.stateAt) < time.Second && a.res != nil
	a.mu.Unlock()
	if fresh {
		return nil
	}
	b, err := a.Request(ctx, http.MethodGet, "", nil)
	if err != nil {
		return err
	}
	var st map[string]any
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("the bridge's v1 state is not a JSON object: %w", err)
	}
	if _, ok := st["lights"]; !ok {
		return errors.New("the bridge's v1 state has no lights (an unauthorised key?)")
	}
	a.mu.Lock()
	a.pending = append(a.pending, a.build(st)...)
	a.mu.Unlock()
	return nil
}

// build turns the v1 state into v2 resources (a.mu held) and returns the
// button presses and motion starts since the last state.
func (a *V1Client) build(st map[string]any) []Event {
	res := map[string][]json.RawMessage{}
	add := func(t string, r map[string]any) {
		r["type"] = t
		res[t] = append(res[t], raw(r))
	}
	a.lightNum, a.groupNum, a.sceneOf = map[string]string{}, map[string]string{}, map[string][2]string{}
	if a.lastBtn == nil {
		a.lastBtn, a.lastMot = map[string]string{}, map[string]bool{}
	}
	cfg, _ := st["config"].(map[string]any)
	add("device", map[string]any{"id": v1BridgeDev, "metadata": map[string]any{"name": str(cfg["name"])},
		"product_data": map[string]any{"product_archetype": "bridge_v2", "product_name": "Hue bridge " + str(cfg["modelid"]), "software_version": str(cfg["swversion"]),
			"model_id": str(cfg["modelid"]), "manufacturer_name": "Signify Netherlands B.V."},
		"services": []any{map[string]any{"rid": v1BridgeSvc, "rtype": "bridge"}}})
	add("bridge", map[string]any{"id": v1BridgeSvc, "bridge_id": str(cfg["bridgeid"]), "owner": map[string]any{"rid": v1BridgeDev, "rtype": "device"}})

	lights, _ := st["lights"].(map[string]any)
	for _, n := range sortedKeys(lights) {
		l, _ := lights[n].(map[string]any)
		s, _ := l["state"].(map[string]any)
		dev, svc, conn := v1LightDev(n), v1LightSvc(n), v1LightConn(n)
		a.lightNum[svc] = n
		add("device", map[string]any{"id": dev, "id_v1": "/lights/" + n, "metadata": map[string]any{"name": str(l["name"]), "archetype": archetypeOfV1(l)},
			"product_data": map[string]any{"product_archetype": archetypeOfV1(l), "product_name": firstNonEmpty(str(l["productname"]), str(l["modelid"])),
				"model_id": str(l["modelid"]), "manufacturer_name": str(l["manufacturername"]), "software_version": str(l["swversion"])},
			"services": []any{map[string]any{"rid": svc, "rtype": "light"}, map[string]any{"rid": conn, "rtype": "zigbee_connectivity"}}})
		lr := map[string]any{"id": svc, "id_v1": "/lights/" + n, "owner": map[string]any{"rid": dev, "rtype": "device"},
			"metadata": map[string]any{"name": str(l["name"]), "archetype": archetypeOfV1(l)}}
		on, _ := s["on"].(bool)
		lr["on"] = map[string]any{"on": on}
		if bri, ok := num(s["bri"]); ok {
			lr["dimming"] = map[string]any{"brightness": round1(bri / 254 * 100)}
		}
		mode := str(s["colormode"])
		if xy, ok := s["xy"].([]any); ok && len(xy) == 2 {
			x, _ := num(xy[0])
			y, _ := num(xy[1])
			lr["color"] = map[string]any{"xy": map[string]any{"x": x, "y": y}}
		}
		if ct, ok := num(s["ct"]); ok {
			sch := map[string]any{"mirek_minimum": 153.0, "mirek_maximum": 500.0}
			if caps, ok := l["capabilities"].(map[string]any); ok {
				ctl, _ := caps["control"].(map[string]any)
				if r, ok := ctl["ct"].(map[string]any); ok {
					if v, ok := num(r["min"]); ok {
						sch["mirek_minimum"] = v
					}
					if v, ok := num(r["max"]); ok {
						sch["mirek_maximum"] = v
					}
				}
			}
			lr["color_temperature"] = map[string]any{"mirek": ct, "mirek_valid": mode == "ct" || mode == "", "mirek_schema": sch}
		}
		add("light", lr)
		status := "connected"
		if r, ok := s["reachable"].(bool); ok && !r {
			status = "connectivity_issue"
		}
		add("zigbee_connectivity", map[string]any{"id": conn, "status": status, "owner": map[string]any{"rid": dev, "rtype": "device"}})
	}

	groups, _ := st["groups"].(map[string]any)
	for _, n := range sortedKeys(groups) {
		g, _ := groups[n].(map[string]any)
		typ := str(g["type"])
		t := "room"
		switch typ {
		case "Room":
		case "Zone":
			t = "zone"
		default:
			continue // LightGroup, Luminaire, Entertainment: not rooms
		}
		var children []any
		ls, _ := g["lights"].([]any)
		for _, x := range ls {
			if ln, ok := x.(string); ok {
				rt, rid := "device", v1LightDev(ln)
				if t == "zone" {
					rt, rid = "light", v1LightSvc(ln)
				}
				children = append(children, map[string]any{"rid": rid, "rtype": rt})
			}
		}
		gid, gl := v1Group(n), v1GroupLight(n)
		a.groupNum[gl] = n
		add(t, map[string]any{"id": gid, "id_v1": "/groups/" + n, "metadata": map[string]any{"name": str(g["name"]), "archetype": strings.ToLower(str(g["class"]))},
			"children": children, "services": []any{map[string]any{"rid": gl, "rtype": "grouped_light"}}})
		gs, _ := g["state"].(map[string]any)
		act, _ := g["action"].(map[string]any)
		anyOn, _ := gs["any_on"].(bool)
		glr := map[string]any{"id": gl, "id_v1": "/groups/" + n, "owner": map[string]any{"rid": gid, "rtype": t}, "on": map[string]any{"on": anyOn}}
		if bri, ok := num(act["bri"]); ok {
			glr["dimming"] = map[string]any{"brightness": round1(bri / 254 * 100)}
		}
		if _, ok := act["xy"]; ok {
			glr["color"] = map[string]any{}
		}
		if _, ok := act["ct"]; ok {
			glr["color_temperature"] = map[string]any{}
		}
		add("grouped_light", glr)
	}

	scenes, _ := st["scenes"].(map[string]any)
	for _, id := range sortedKeys(scenes) {
		s, _ := scenes[id].(map[string]any)
		g := str(s["group"])
		if g == "" || groups[g] == nil {
			continue // a light scene without a room
		}
		t := "room"
		if gm, _ := groups[g].(map[string]any); str(gm["type"]) == "Zone" {
			t = "zone"
		}
		sid := v1Scene(id)
		a.sceneOf[sid] = [2]string{id, g}
		add("scene", map[string]any{"id": sid, "id_v1": "/scenes/" + id, "metadata": map[string]any{"name": str(s["name"])},
			"group": map[string]any{"rid": v1Group(g), "rtype": t}, "status": map[string]any{"active": "inactive"}})
	}

	// Sensors: one physical device has several v1 sensors with the same
	// MAC at the start of their uniqueid.
	var evs []Event
	sensors, _ := st["sensors"].(map[string]any)
	type physical struct {
		name, model, maker, fw string
		services               []any
	}
	devs := map[string]*physical{}
	var order []string
	for _, n := range sortedKeys(sensors) {
		s, _ := sensors[n].(map[string]any)
		uid := str(s["uniqueid"])
		mac, _, _ := strings.Cut(uid, "-")
		if mac == "" {
			continue // a virtual sensor (CLIP, daylight)
		}
		typ := str(s["type"])
		state, _ := s["state"].(map[string]any)
		scfg, _ := s["config"].(map[string]any)
		dev := v1SensorDev(mac)
		p := devs[dev]
		if p == nil {
			p = &physical{model: str(s["productname"]), maker: str(s["manufacturername"]), fw: str(s["swversion"])}
			devs[dev] = p
			order = append(order, dev)
		}
		svc := dev + "-" + n
		owner := map[string]any{"rid": dev, "rtype": "device"}
		upd := str(state["lastupdated"])
		switch typ {
		case "ZLLPresence":
			p.name = str(s["name"])
			pres, _ := state["presence"].(bool)
			add("motion", map[string]any{"id": svc, "owner": owner, "motion": map[string]any{"motion": pres, "motion_valid": true}})
			p.services = append(p.services, map[string]any{"rid": svc, "rtype": "motion"})
			if was, seen := a.lastMot[n]; seen && pres && !was {
				evs = append(evs, Event{Type: "update", Created: time.Now().UTC(), Data: []Resource{{"id": svc, "type": "motion", "owner": owner,
					"motion": map[string]any{"motion": true, "motion_valid": true}}}})
			}
			a.lastMot[n] = pres
		case "ZLLTemperature":
			r := map[string]any{"id": svc, "owner": owner}
			if t, ok := num(state["temperature"]); ok {
				r["temperature"] = map[string]any{"temperature": t / 100, "temperature_valid": true}
			}
			add("temperature", r)
			p.services = append(p.services, map[string]any{"rid": svc, "rtype": "temperature"})
		case "ZLLLightLevel":
			r := map[string]any{"id": svc, "owner": owner}
			if l, ok := num(state["lightlevel"]); ok {
				r["light"] = map[string]any{"light_level": l, "light_level_valid": true}
			}
			add("light_level", r)
			p.services = append(p.services, map[string]any{"rid": svc, "rtype": "light_level"})
		case "ZLLSwitch", "ZGPSwitch", "ZLLRelativeRotary":
			p.name = firstNonEmpty(p.name, str(s["name"]))
			code, _ := num(state["buttonevent"])
			button, event := V1ButtonEvent(int(code), typ)
			r := map[string]any{"id": svc, "owner": owner, "metadata": map[string]any{"control_id": float64(button)},
				"button": map[string]any{"last_event": event}}
			add("button", r)
			p.services = append(p.services, map[string]any{"rid": svc, "rtype": "button"})
			if old, seen := a.lastBtn[n]; seen && upd != old && upd != "" && upd != "none" && event != "" {
				evs = append(evs, Event{Type: "update", Created: time.Now().UTC(), Data: []Resource{{"id": svc, "type": "button", "owner": owner,
					"metadata": map[string]any{"control_id": float64(button)}, "button": map[string]any{"button_report": map[string]any{"event": event, "updated": upd}}}}})
			}
			a.lastBtn[n] = upd
		default:
			continue
		}
		if b, ok := num(scfg["battery"]); ok {
			id := dev + "-power"
			found := false
			for _, x := range p.services {
				if x.(map[string]any)["rid"] == id {
					found = true
				}
			}
			if !found {
				add("device_power", map[string]any{"id": id, "owner": owner, "power_state": map[string]any{"battery_level": b}})
				p.services = append(p.services, map[string]any{"rid": id, "rtype": "device_power"})
			}
		}
		if p.name == "" {
			p.name = str(s["name"])
		}
	}
	for _, dev := range order {
		p := devs[dev]
		add("device", map[string]any{"id": dev, "metadata": map[string]any{"name": p.name},
			"product_data": map[string]any{"product_name": p.model, "manufacturer_name": p.maker, "software_version": p.fw}, "services": p.services})
	}
	a.state, a.stateAt, a.res = st, time.Now(), res
	return evs
}

// V1ButtonEvent reads a v1 buttonevent code: the button in the thousands
// digit, then 0 initial press, 1 repeat (held), 2 short release, 3 long
// release; Hue tap switches send fixed codes (34, 16, 17, 18).
func V1ButtonEvent(code int, typ string) (int, string) {
	if typ == "ZGPSwitch" {
		switch code {
		case 34:
			return 1, "short_release"
		case 16:
			return 2, "short_release"
		case 17:
			return 3, "short_release"
		case 18:
			return 4, "short_release"
		}
	}
	if code < 1000 {
		return 1, ""
	}
	button := code / 1000
	switch code % 1000 {
	case 0:
		return button, "initial_press"
	case 1:
		return button, "repeat"
	case 2:
		return button, "short_release"
	case 3:
		return button, "long_release"
	}
	return button, ""
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Do serves CLIP v2 requests from API v1: a GET of a resource type from
// the v1 state (Refresh), a PUT of a light, grouped_light or scene (a
// recall) as the v1 request that does the same. Other requests are an
// *APIError.
func (a *V1Client) Do(ctx context.Context, method, path string, body any) ([]json.RawMessage, error) {
	const prefix = "/clip/v2/resource/"
	if !strings.HasPrefix(path, prefix) {
		return nil, &APIError{Status: http.StatusNotFound, Desc: []string{"not served by API v1: " + path}}
	}
	rest := strings.TrimPrefix(path, prefix)
	rtype, id, _ := strings.Cut(rest, "/")
	if method == http.MethodGet {
		if err := a.Refresh(ctx); err != nil {
			return nil, err
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.res[rtype], nil
	}
	if method != http.MethodPut {
		return nil, &APIError{Status: http.StatusMethodNotAllowed}
	}
	m, _ := body.(map[string]any)
	a.mu.Lock()
	ln, isLight := a.lightNum[id]
	gn, isGroup := a.groupNum[id]
	sc, isScene := a.sceneOf[id]
	a.mu.Unlock()
	var err error
	switch {
	case rtype == "light" && isLight:
		_, err = a.Request(ctx, http.MethodPut, "/lights/"+ln+"/state", V1State(m))
	case rtype == "grouped_light" && isGroup:
		_, err = a.Request(ctx, http.MethodPut, "/groups/"+gn+"/action", V1State(m))
	case rtype == "scene" && isScene:
		_, err = a.Request(ctx, http.MethodPut, "/groups/"+sc[1]+"/action", map[string]any{"scene": sc[0]})
	default:
		return nil, &APIError{Status: http.StatusNotFound, Desc: []string{fmt.Sprintf("no v1 %s for %s", rtype, id)}}
	}
	a.mu.Lock()
	a.stateAt = time.Time{} // read again next time
	a.mu.Unlock()
	return nil, err
}

// V1State turns a v2 light or grouped_light body into a v1 state body:
// on, bri (1-254), xy, ct and transitiontime (tenths of a second).
func V1State(m map[string]any) map[string]any {
	out := map[string]any{}
	get := func(obj, key string) (any, bool) {
		o, ok := m[obj].(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := o[key]
		return v, ok
	}
	if v, ok := get("on", "on"); ok {
		out["on"] = v
	}
	if v, ok := get("dimming", "brightness"); ok {
		if f, ok := v.(float64); ok {
			out["bri"] = int(math.Max(1, math.Min(254, math.Round(f/100*254))))
		}
	}
	if v, ok := get("color", "xy"); ok {
		if xy, ok := v.(map[string]any); ok {
			out["xy"] = []any{xy["x"], xy["y"]}
		}
	}
	if v, ok := get("color_temperature", "mirek"); ok {
		out["ct"] = v
	}
	if v, ok := get("dynamics", "duration"); ok {
		switch d := v.(type) {
		case float64:
			out["transitiontime"] = int(math.Round(d / 100))
		case int:
			out["transitiontime"] = (d + 50) / 100
		}
	}
	return out
}

// V1Base is the HTTP base of a v1 bridge at an address.
func V1Base(address string) string {
	a := strings.TrimSpace(address)
	a = strings.TrimPrefix(strings.TrimPrefix(a, "https://"), "http://")
	a = strings.TrimRight(a, "/")
	if _, _, err := net.SplitHostPort(a); err != nil {
		if ip := net.ParseIP(strings.Trim(a, "[]")); ip != nil && ip.To4() == nil {
			a = "[" + strings.Trim(a, "[]") + "]"
		}
	}
	return "http://" + a
}

// V1Config is the public part of a bridge's configuration
// (GET /api/0/config, no key needed).
type V1Config struct {
	// Name is the bridge's name.
	Name string `json:"name"`
	// BridgeID is the bridge ID as the bridge writes it (upper case).
	BridgeID string `json:"bridgeid"`
	// ModelID is the bridge model (BSB001 round, BSB002 square, …).
	ModelID string `json:"modelid"`
	// APIVersion is the API version (1.46 and later have CLIP v2).
	APIVersion string `json:"apiversion"`
	// SWVersion is the firmware version.
	SWVersion string `json:"swversion"`
}

// ErrNotBridge is returned by ReadConfig when the answer is not a
// bridge's configuration.
var ErrNotBridge = errors.New("the address does not answer as a Hue bridge")

// ReadConfig reads a bridge's public configuration (GET /api/0/config,
// no key needed) at a base URL, http or https. A transport failure is
// returned as hc returns it; an answer other than 200 with a JSON object
// is ErrNotBridge.
func ReadConfig(ctx context.Context, hc *http.Client, base string) (V1Config, error) {
	var cfg V1Config
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/0/config", nil)
	if err != nil {
		return cfg, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return cfg, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&cfg); err != nil || resp.StatusCode != http.StatusOK {
		return cfg, ErrNotBridge
	}
	return cfg, nil
}

// ReadV1Config reads a bridge's public configuration over HTTP at an
// address; an answer without a bridge ID is an error.
func ReadV1Config(ctx context.Context, hc *http.Client, address string) (V1Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, V1Base(address)+"/api/0/config", nil)
	if err != nil {
		return V1Config{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return V1Config{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var c V1Config
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&c); err != nil || c.BridgeID == "" {
		return V1Config{}, ErrNotBridge
	}
	return c, nil
}

// NeedsV1 tells a bridge that speaks only API v1: the round bridge
// (BSB001) and firmware without CLIP v2 (API version below 1.46).
func NeedsV1(c V1Config) bool {
	if strings.EqualFold(c.ModelID, "BSB001") {
		return true
	}
	p := strings.SplitN(c.APIVersion, ".", 3)
	if len(p) < 2 {
		return false
	}
	maj, _ := strconv.Atoi(p[0])
	mnr, _ := strconv.Atoi(p[1])
	return maj < 1 || (maj == 1 && mnr < 46)
}
