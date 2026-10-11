// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"context"
	"encoding/json"
)

// Typed views of common CLIP v2 resources, for callers that want field
// checks at compile time. Resource (a map) stays the model the client
// returns: it carries every field, including those of resource types and
// firmware versions these structs do not describe. Convert with As or
// ListAs. Optional fields are pointers; fields a struct does not name are
// dropped by the conversion, not by the client.
//
// The field names follow the bridge's JSON; they were cross-checked
// against the openhue-api OpenAPI description (Apache-2.0, see NOTICE).
// The structs were written for this module; nothing was generated.

// Identifier is a reference as the bridge's JSON carries it.
type Identifier struct {
	RID   string `json:"rid"`
	RType string `json:"rtype"`
}

// Ref is the identifier as a Ref.
func (i Identifier) Ref() Ref { return Ref{RID: i.RID, RType: i.RType} }

// Metadata is a resource's name and kind.
type Metadata struct {
	Name      string      `json:"name,omitempty"`
	Archetype string      `json:"archetype,omitempty"`
	Function  string      `json:"function,omitempty"`
	Image     *Identifier `json:"image,omitempty"`
}

// OnState is the on/off state.
type OnState struct {
	On bool `json:"on"`
}

// Dimming is a brightness in percent.
type Dimming struct {
	Brightness  float64  `json:"brightness"`
	MinDimLevel *float64 `json:"min_dim_level,omitempty"`
}

// XY is a CIE 1931 colour point.
type XY struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Gamut is the colour triangle a light can show.
type Gamut struct {
	Red   XY `json:"red"`
	Green XY `json:"green"`
	Blue  XY `json:"blue"`
}

// Colour is a light's colour.
type Colour struct {
	XY        XY     `json:"xy"`
	Gamut     *Gamut `json:"gamut,omitempty"`
	GamutType string `json:"gamut_type,omitempty"`
}

// MirekSchema is the colour temperature range of a light.
type MirekSchema struct {
	Minimum int `json:"mirek_minimum"`
	Maximum int `json:"mirek_maximum"`
}

// ColourTemperature is a light's white colour temperature in mirek.
type ColourTemperature struct {
	Mirek       *int         `json:"mirek"`
	MirekValid  bool         `json:"mirek_valid"`
	MirekSchema *MirekSchema `json:"mirek_schema,omitempty"`
}

// Dynamics is a light's transition state.
type Dynamics struct {
	Status     string  `json:"status,omitempty"`
	Speed      float64 `json:"speed"`
	SpeedValid bool    `json:"speed_valid"`
}

// Light is a light service.
type Light struct {
	ID               string             `json:"id"`
	IDv1             string             `json:"id_v1,omitempty"`
	Type             string             `json:"type"`
	Owner            Identifier         `json:"owner"`
	Metadata         Metadata           `json:"metadata"`
	On               OnState            `json:"on"`
	Dimming          *Dimming           `json:"dimming,omitempty"`
	ColorTemperature *ColourTemperature `json:"color_temperature,omitempty"`
	Color            *Colour            `json:"color,omitempty"`
	Dynamics         *Dynamics          `json:"dynamics,omitempty"`
	Mode             string             `json:"mode,omitempty"`
}

// Group is a room or a zone.
type Group struct {
	ID       string       `json:"id"`
	IDv1     string       `json:"id_v1,omitempty"`
	Type     string       `json:"type"`
	Metadata Metadata     `json:"metadata"`
	Children []Identifier `json:"children"`
	Services []Identifier `json:"services"`
}

// GroupedLight is the light service of a room, zone or the whole home.
type GroupedLight struct {
	ID      string     `json:"id"`
	IDv1    string     `json:"id_v1,omitempty"`
	Type    string     `json:"type"`
	Owner   Identifier `json:"owner"`
	On      *OnState   `json:"on,omitempty"`
	Dimming *Dimming   `json:"dimming,omitempty"`
}

// SceneActionJSON is one light setting of a scene.
type SceneActionJSON struct {
	Target Identifier     `json:"target"`
	Action map[string]any `json:"action"`
}

// SceneStatus is whether a scene is active.
type SceneStatus struct {
	Active string `json:"active"`
}

// Scene is a scene of a room or zone.
type Scene struct {
	ID          string            `json:"id"`
	IDv1        string            `json:"id_v1,omitempty"`
	Type        string            `json:"type"`
	Metadata    Metadata          `json:"metadata"`
	Group       Identifier        `json:"group"`
	Actions     []SceneActionJSON `json:"actions"`
	Speed       float64           `json:"speed"`
	AutoDynamic bool              `json:"auto_dynamic"`
	Status      *SceneStatus      `json:"status,omitempty"`
}

// ProductData is what a device says about itself.
type ProductData struct {
	ModelID          string `json:"model_id"`
	ManufacturerName string `json:"manufacturer_name"`
	ProductName      string `json:"product_name"`
	ProductArchetype string `json:"product_archetype"`
	Certified        bool   `json:"certified"`
	SoftwareVersion  string `json:"software_version"`
}

// Device is a physical device and its services.
type Device struct {
	ID          string       `json:"id"`
	IDv1        string       `json:"id_v1,omitempty"`
	Type        string       `json:"type"`
	ProductData ProductData  `json:"product_data"`
	Metadata    Metadata     `json:"metadata"`
	Services    []Identifier `json:"services"`
}

// BehaviorInstance is an automation: a behavior_script run with a
// configuration.
type BehaviorInstance struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	ScriptID      string         `json:"script_id"`
	Enabled       bool           `json:"enabled"`
	State         map[string]any `json:"state,omitempty"`
	Configuration map[string]any `json:"configuration"`
	Dependees     []any          `json:"dependees,omitempty"`
	Status        string         `json:"status,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	Metadata      Metadata       `json:"metadata"`
}

// Position is a point in an entertainment area (each axis -1 to 1).
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// ChannelMember is a light segment of an entertainment channel.
type ChannelMember struct {
	Service Identifier `json:"service"`
	Index   int        `json:"index"`
}

// EntertainmentChannel is one channel of an entertainment configuration:
// what one entry of a stream frame colours.
type EntertainmentChannel struct {
	ChannelID int             `json:"channel_id"`
	Position  Position        `json:"position"`
	Members   []ChannelMember `json:"members"`
}

// ServiceLocation places an entertainment service in the area.
type ServiceLocation struct {
	Service            Identifier `json:"service"`
	Positions          []Position `json:"positions"`
	EqualizationFactor float64    `json:"equalization_factor,omitempty"`
}

// EntertainmentConfiguration is an entertainment area.
type EntertainmentConfiguration struct {
	ID                string                 `json:"id"`
	IDv1              string                 `json:"id_v1,omitempty"`
	Type              string                 `json:"type"`
	Metadata          Metadata               `json:"metadata"`
	ConfigurationType string                 `json:"configuration_type"`
	Status            string                 `json:"status"`
	ActiveStreamer    *Identifier            `json:"active_streamer,omitempty"`
	Channels          []EntertainmentChannel `json:"channels"`
	LightServices     []Identifier           `json:"light_services"`
	Locations         struct {
		ServiceLocations []ServiceLocation `json:"service_locations"`
	} `json:"locations"`
}

// As converts a resource to a typed view (Light, Group, Scene, …).
func As[T any](r Resource) (T, error) {
	var out T
	b, err := json.Marshal(r)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// ListAs lists the resources of a type as typed views.
func ListAs[T any](ctx context.Context, c *Client, rtype string) ([]T, error) {
	list, err := c.List(ctx, rtype)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(list))
	for _, r := range list {
		v, err := As[T](r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
