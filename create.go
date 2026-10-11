// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Creating, changing and deleting CLIP v2 resources. A bridge answers a
// POST to /clip/v2/resource/<type> with the new resource's reference
// ({"rid", "rtype"}) and a DELETE of /clip/v2/resource/<type>/<id> with
// the reference of the deleted one; failures are the usual error answers.
// The bridge itself checks references (a room's devices, a scene's
// lights) and refuses what it cannot apply.

// ErrNoReference is returned by Post when the bridge answered without a
// reference to the new resource.
var ErrNoReference = errors.New("the bridge's answer names no resource")

// Get reads one resource (GET /clip/v2/resource/<type>/<id>). A missing
// resource is an *APIError with status 404 on a bridge; some emulators
// answer an empty list, which is returned as such an error too.
func (c *Client) Get(ctx context.Context, rtype, id string) (Resource, error) {
	items, err := c.Do(ctx, http.MethodGet, ResourcePath+rtype+"/"+id, nil)
	if err != nil {
		return nil, err
	}
	var only Resource
	for _, raw := range items {
		var r Resource
		if json.Unmarshal(raw, &r) == nil && r.ID() == id {
			return r, nil
		}
		only = r
	}
	if len(items) == 1 && only != nil {
		// The one resource answered for the ID (an emulator that lists a
		// resource under another ID than it gave it on creation).
		return only, nil
	}
	return nil, &APIError{Status: http.StatusNotFound, Desc: []string{fmt.Sprintf("no %s %s", rtype, id)}}
}

// Post creates a resource (POST /clip/v2/resource/<type>) and returns the
// reference the bridge gave it.
func (c *Client) Post(ctx context.Context, rtype string, body any) (Ref, error) {
	items, err := c.Do(ctx, http.MethodPost, ResourcePath+rtype, body)
	if err != nil {
		return Ref{}, err
	}
	for _, raw := range items {
		var r struct {
			RID   string `json:"rid"`
			RType string `json:"rtype"`
		}
		if json.Unmarshal(raw, &r) == nil && r.RID != "" {
			if r.RType == "" {
				r.RType = rtype
			}
			return Ref{RID: r.RID, RType: r.RType}, nil
		}
	}
	return Ref{}, ErrNoReference
}

// Delete removes a resource (DELETE /clip/v2/resource/<type>/<id>).
func (c *Client) Delete(ctx context.Context, rtype, id string) error {
	_, err := c.Do(ctx, http.MethodDelete, ResourcePath+rtype+"/"+id, nil)
	return err
}

// RefBody is a reference as CLIP v2 bodies carry it.
func RefBody(r Ref) map[string]any { return map[string]any{"rid": r.RID, "rtype": r.RType} }

func refList(refs []Ref) []any {
	out := make([]any, 0, len(refs))
	for _, r := range refs {
		out = append(out, RefBody(r))
	}
	return out
}

// GroupSpec describes a room or a zone. A room's children are devices; a
// zone's are light services (or devices). An empty field is left out of
// an update.
type GroupSpec struct {
	// Name is the group's name (metadata.name).
	Name string
	// Archetype is the group's kind (metadata.archetype), such as
	// living_room or bedroom; "other" when empty on creation.
	Archetype string
	// Children are the group's members.
	Children []Ref
}

// Body is the CLIP v2 body of the spec; create fills defaults.
func (s GroupSpec) Body(create bool) map[string]any {
	b := map[string]any{}
	md := map[string]any{}
	if s.Name != "" {
		md["name"] = s.Name
	}
	switch {
	case s.Archetype != "":
		md["archetype"] = s.Archetype
	case create:
		md["archetype"] = "other"
	}
	if len(md) > 0 {
		b["metadata"] = md
	}
	if s.Children != nil || create {
		b["children"] = refList(s.Children)
	}
	return b
}

// CreateRoom creates a room of devices.
func (c *Client) CreateRoom(ctx context.Context, s GroupSpec) (Ref, error) {
	if s.Name == "" {
		return Ref{}, errors.New("a room needs a name")
	}
	return c.Post(ctx, "room", s.Body(true))
}

// CreateZone creates a zone of lights.
func (c *Client) CreateZone(ctx context.Context, s GroupSpec) (Ref, error) {
	if s.Name == "" {
		return Ref{}, errors.New("a zone needs a name")
	}
	return c.Post(ctx, "zone", s.Body(true))
}

// UpdateGroup changes a room or zone (rtype "room" or "zone"): the fields
// set in s.
func (c *Client) UpdateGroup(ctx context.Context, rtype, id string, s GroupSpec) error {
	return c.Put(ctx, rtype, id, s.Body(false))
}

// SceneAction is what a scene does to one light when recalled: a light
// body such as {"on": {"on": true}, "dimming": {"brightness": 50}}.
type SceneAction struct {
	// Target is the light service.
	Target Ref
	// Action is the light body.
	Action map[string]any
}

// SceneSpec describes a scene. An empty field is left out of an update.
type SceneSpec struct {
	// Name is the scene's name (metadata.name).
	Name string
	// Group is the room or zone the scene belongs to.
	Group Ref
	// Actions are the per-light settings.
	Actions []SceneAction
	// Speed is the speed of the scene's dynamic palette (0 to 1); nil
	// leaves it out.
	Speed *float64
}

// Body is the CLIP v2 body of the spec.
func (s SceneSpec) Body() map[string]any {
	b := map[string]any{}
	if s.Name != "" {
		b["metadata"] = map[string]any{"name": s.Name}
	}
	if s.Group.RID != "" {
		b["group"] = RefBody(s.Group)
	}
	if s.Actions != nil {
		acts := make([]any, 0, len(s.Actions))
		for _, a := range s.Actions {
			acts = append(acts, map[string]any{"target": RefBody(a.Target), "action": a.Action})
		}
		b["actions"] = acts
	}
	if s.Speed != nil {
		b["speed"] = *s.Speed
	}
	return b
}

// CreateScene creates a scene of a room or zone.
func (c *Client) CreateScene(ctx context.Context, s SceneSpec) (Ref, error) {
	if s.Name == "" || s.Group.RID == "" {
		return Ref{}, errors.New("a scene needs a name and a group")
	}
	if s.Actions == nil {
		s.Actions = []SceneAction{}
	}
	return c.Post(ctx, "scene", s.Body())
}

// UpdateScene changes a scene: the fields set in s (the group cannot be
// changed and is left out).
func (c *Client) UpdateScene(ctx context.Context, id string, s SceneSpec) error {
	s.Group = Ref{}
	return c.Put(ctx, "scene", id, s.Body())
}

// RecallScene activates a scene (action "active", "static" or
// "dynamic_palette"; "active" when empty).
func (c *Client) RecallScene(ctx context.Context, id, action string) error {
	if action == "" {
		action = "active"
	}
	return c.Put(ctx, "scene", id, map[string]any{"recall": map[string]any{"action": action}})
}

// BehaviorSpec describes a behaviour (a behavior_instance: an automation
// that runs one of the bridge's behavior_script resources with a
// configuration). An empty field is left out of an update, except
// Enabled, which an update always sends.
type BehaviorSpec struct {
	// Name is the behaviour's name (metadata.name).
	Name string
	// ScriptID is the behavior_script it runs (creation only).
	ScriptID string
	// Enabled turns the behaviour on.
	Enabled bool
	// Configuration is the script's configuration, in the shape the
	// script's configuration_schema describes.
	Configuration map[string]any
}

// Body is the CLIP v2 body of the spec.
func (s BehaviorSpec) Body(create bool) map[string]any {
	b := map[string]any{"enabled": s.Enabled}
	if s.Name != "" {
		b["metadata"] = map[string]any{"name": s.Name}
	}
	if create && s.ScriptID != "" {
		b["script_id"] = s.ScriptID
	}
	if s.Configuration != nil {
		b["configuration"] = s.Configuration
	}
	return b
}

// CreateBehavior creates a behaviour.
func (c *Client) CreateBehavior(ctx context.Context, s BehaviorSpec) (Ref, error) {
	if s.ScriptID == "" {
		return Ref{}, errors.New("a behaviour needs a script ID")
	}
	if s.Configuration == nil {
		s.Configuration = map[string]any{}
	}
	return c.Post(ctx, "behavior_instance", s.Body(true))
}

// UpdateBehavior changes a behaviour.
func (c *Client) UpdateBehavior(ctx context.Context, id string, s BehaviorSpec) error {
	return c.Put(ctx, "behavior_instance", id, s.Body(false))
}

// ApplicationIDHeader is the header in which GET /auth/v1 names the
// application ID of a key: the identity of entertainment streaming.
const ApplicationIDHeader = "hue-application-id"

// ApplicationID reads the application ID of the client's key
// (GET /auth/v1, header hue-application-id).
func (c *Client) ApplicationID(ctx context.Context) (string, error) {
	if c.V1 != nil {
		return "", errors.New("API v1 has no application ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/auth/v1", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(KeyHeader, c.key)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", Scrub(err, c.key)
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w (HTTP %d)", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode/100 != 2:
		return "", &APIError{Status: resp.StatusCode}
	}
	id := strings.TrimSpace(resp.Header.Get(ApplicationIDHeader))
	if id == "" {
		return "", errors.New("the bridge's answer to /auth/v1 has no application ID")
	}
	return id, nil
}

// StartEntertainment starts streaming to an entertainment configuration
// (PUT {"action": "start"}); the bridge then expects the DTLS stream.
func (c *Client) StartEntertainment(ctx context.Context, configID string) error {
	return c.Put(ctx, "entertainment_configuration", configID, map[string]any{"action": "start"})
}

// StopEntertainment ends streaming to an entertainment configuration.
func (c *Client) StopEntertainment(ctx context.Context, configID string) error {
	return c.Put(ctx, "entertainment_configuration", configID, map[string]any{"action": "stop"})
}
