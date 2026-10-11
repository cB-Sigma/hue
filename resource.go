// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"encoding/json"
	"math"
	"strings"
)

// Resource is a CLIP v2 resource as the bridge sends it: a JSON object
// with at least "id" and "type". Updates from the event stream are partial
// resources that Merge applies to the full one.
type Resource map[string]any

// ID is the resource's ID.
func (r Resource) ID() string { s, _ := r["id"].(string); return s }

// Type is the resource's type (light, device, room, …).
func (r Resource) Type() string { s, _ := r["type"].(string); return s }

// Ref is a reference to the resource.
func (r Resource) Ref() Ref { return Ref{RID: r.ID(), RType: r.Type()} }

// Get follows a path of object keys and returns the value there, or nil.
func (r Resource) Get(path ...string) any {
	var cur any = map[string]any(r)
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// Str is the string at a path ("" when absent or not a string).
func (r Resource) Str(path ...string) string { s, _ := r.Get(path...).(string); return s }

// Num is the number at a path; ok is false when absent or not a number.
func (r Resource) Num(path ...string) (float64, bool) {
	f, ok := r.Get(path...).(float64)
	return f, ok
}

// Bool is the boolean at a path; ok is false when absent or not a
// boolean.
func (r Resource) Bool(path ...string) (bool, bool) {
	b, ok := r.Get(path...).(bool)
	return b, ok
}

// Owner is the rid of the resource's owner (the device of a service).
func (r Resource) Owner() string { return r.Str("owner", "rid") }

// Refs lists the {rid, rtype} references of a list field, such as
// "services" or "children".
func (r Resource) Refs(field string) []Ref {
	list, _ := r[field].([]any)
	out := make([]Ref, 0, len(list))
	for _, x := range list {
		m, _ := x.(map[string]any)
		rid, _ := m["rid"].(string)
		rt, _ := m["rtype"].(string)
		if rid != "" {
			out = append(out, Ref{rid, rt})
		}
	}
	return out
}

// Clone is a deep copy of the resource.
func (r Resource) Clone() Resource {
	b, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// Ref is a reference to another resource.
type Ref struct{ RID, RType string }

// Merge applies a partial resource to dst: objects are merged
// recursively, other values replaced.
func Merge(dst, src map[string]any) {
	for k, v := range src {
		sm, sok := v.(map[string]any)
		dm, dok := dst[k].(map[string]any)
		if sok && dok {
			Merge(dm, sm)
			continue
		}
		dst[k] = v
	}
}

// LastButtonEvent is a button service's last event (initial_press,
// short_release, long_press, …): the button report's when the bridge sends
// one, else last_event.
func LastButtonEvent(r Resource) string {
	if e := r.Str("button", "button_report", "event"); e != "" {
		return e
	}
	return r.Str("button", "last_event")
}

// LuxFromLightLevel converts a light_level value, which is
// 10000·log10(lux)+1, to lux rounded to one decimal.
func LuxFromLightLevel(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(math.Pow(10, (v-1)/10000)*10) / 10
}

// NormaliseBridgeID lower-cases a bridge ID and drops separators (":",
// "-", spaces), so "00:17:88:FF:FE:4A:1B:2C" becomes "001788fffe4a1b2c".
func NormaliseBridgeID(s string) string {
	return strings.ToLower(strings.NewReplacer(":", "", "-", "", " ", "").Replace(strings.TrimSpace(s)))
}
