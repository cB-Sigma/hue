// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

// Typed views of the CLIP v2 sensor, switch, connectivity, bridge and
// automation resources, next to those in models.go and used the same way
// (As, ListAs). Times are kept as the bridge writes them (RFC 3339 text),
// so that a firmware or emulator writing another form does not make the
// whole conversion fail. Readings come in two forms: the report (with the
// time of the change), which newer firmware sends, and the older plain
// value; the Value methods prefer the report.
//
// The field names follow the bridge's JSON; they were cross-checked
// against the openhue-api OpenAPI description and aiohue's models (both
// Apache-2.0, see NOTICE). The structs were written for this module.

// MotionReport is a motion reading and when it changed.
type MotionReport struct {
	Changed string `json:"changed"`
	Motion  bool   `json:"motion"`
}

// MotionFeature is the motion state of a sensor or a group of sensors.
type MotionFeature struct {
	Motion       *bool         `json:"motion,omitempty"`
	MotionValid  *bool         `json:"motion_valid,omitempty"`
	MotionReport *MotionReport `json:"motion_report,omitempty"`
}

// Value is whether motion is detected; ok is false when the resource
// carries no reading.
func (m *MotionFeature) Value() (motion, ok bool) {
	switch {
	case m == nil:
		return false, false
	case m.MotionReport != nil:
		return m.MotionReport.Motion, true
	case m.Motion != nil && (m.MotionValid == nil || *m.MotionValid):
		return *m.Motion, true
	}
	return false, false
}

// MotionSensitivity is a motion sensor's sensitivity setting.
type MotionSensitivity struct {
	Sensitivity    int    `json:"sensitivity"`
	SensitivityMax int    `json:"sensitivity_max,omitempty"`
	Status         string `json:"status,omitempty"`
}

// Motion is a motion sensor service.
type Motion struct {
	ID          string             `json:"id"`
	IDv1        string             `json:"id_v1,omitempty"`
	Type        string             `json:"type"`
	Owner       Identifier         `json:"owner"`
	Enabled     bool               `json:"enabled"`
	Motion      *MotionFeature     `json:"motion,omitempty"`
	Sensitivity *MotionSensitivity `json:"sensitivity,omitempty"`
}

// CameraMotion is the motion service of a camera (same shape as Motion).
type CameraMotion = Motion

// GroupedMotion is the combined motion of a room's or zone's sensors.
type GroupedMotion struct {
	ID      string         `json:"id"`
	IDv1    string         `json:"id_v1,omitempty"`
	Type    string         `json:"type"`
	Owner   Identifier     `json:"owner"`
	Enabled bool           `json:"enabled"`
	Motion  *MotionFeature `json:"motion,omitempty"`
}

// TemperatureReport is a temperature reading (°C) and when it changed.
type TemperatureReport struct {
	Changed     string  `json:"changed"`
	Temperature float64 `json:"temperature"`
}

// TemperatureFeature is a temperature state.
type TemperatureFeature struct {
	Temperature       *float64           `json:"temperature,omitempty"`
	TemperatureValid  *bool              `json:"temperature_valid,omitempty"`
	TemperatureReport *TemperatureReport `json:"temperature_report,omitempty"`
}

// Temperature is a temperature sensor service.
type Temperature struct {
	ID          string             `json:"id"`
	IDv1        string             `json:"id_v1,omitempty"`
	Type        string             `json:"type"`
	Owner       Identifier         `json:"owner"`
	Enabled     bool               `json:"enabled"`
	Temperature TemperatureFeature `json:"temperature"`
}

// Value is the temperature in °C; ok is false without a reading.
func (t Temperature) Value() (celsius float64, ok bool) {
	f := t.Temperature
	switch {
	case f.TemperatureReport != nil:
		return f.TemperatureReport.Temperature, true
	case f.Temperature != nil && (f.TemperatureValid == nil || *f.TemperatureValid):
		return *f.Temperature, true
	}
	return 0, false
}

// LightLevelReport is a light level reading (10000·log10(lux)+1) and when
// it changed.
type LightLevelReport struct {
	Changed    string `json:"changed"`
	LightLevel int    `json:"light_level"`
}

// LightLevelFeature is a light level state.
type LightLevelFeature struct {
	LightLevel       *int              `json:"light_level,omitempty"`
	LightLevelValid  *bool             `json:"light_level_valid,omitempty"`
	LightLevelReport *LightLevelReport `json:"light_level_report,omitempty"`
}

// Value is the light level on the bridge's scale; ok is false without a
// reading. LuxFromLightLevel turns it into lux.
func (f LightLevelFeature) Value() (level int, ok bool) {
	switch {
	case f.LightLevelReport != nil:
		return f.LightLevelReport.LightLevel, true
	case f.LightLevel != nil && (f.LightLevelValid == nil || *f.LightLevelValid):
		return *f.LightLevel, true
	}
	return 0, false
}

// LightLevel is a light sensor service.
type LightLevel struct {
	ID      string            `json:"id"`
	IDv1    string            `json:"id_v1,omitempty"`
	Type    string            `json:"type"`
	Owner   Identifier        `json:"owner"`
	Enabled bool              `json:"enabled"`
	Light   LightLevelFeature `json:"light"`
}

// GroupedLightLevel is the combined light level of a room's or zone's
// sensors.
type GroupedLightLevel = LightLevel

// ButtonReport is a button event and when it happened.
type ButtonReport struct {
	Updated string `json:"updated"`
	Event   string `json:"event"`
}

// ButtonFeature is a button's state: its last event and the events it
// can send (initial_press, repeat, short_release, long_press,
// long_release, double_short_release).
type ButtonFeature struct {
	LastEvent      string        `json:"last_event,omitempty"`
	ButtonReport   *ButtonReport `json:"button_report,omitempty"`
	RepeatInterval *int          `json:"repeat_interval,omitempty"`
	EventValues    []string      `json:"event_values,omitempty"`
}

// ButtonMetadata numbers a button on its device.
type ButtonMetadata struct {
	ControlID int `json:"control_id"`
}

// Button is one button of a switch.
type Button struct {
	ID       string         `json:"id"`
	IDv1     string         `json:"id_v1,omitempty"`
	Type     string         `json:"type"`
	Owner    Identifier     `json:"owner"`
	Metadata ButtonMetadata `json:"metadata"`
	Button   *ButtonFeature `json:"button,omitempty"`
}

// Event is the button's last event ("" when none is known).
func (b Button) Event() string {
	switch {
	case b.Button == nil:
		return ""
	case b.Button.ButtonReport != nil:
		return b.Button.ButtonReport.Event
	}
	return b.Button.LastEvent
}

// RotaryRotation is a turn of a dial: its direction (clock_wise,
// counter_clock_wise), steps (1000 a full turn) and duration in ms.
type RotaryRotation struct {
	Direction string `json:"direction"`
	Steps     int    `json:"steps"`
	Duration  int    `json:"duration"`
}

// RotaryEvent is a dial event: action start or repeat, and the turn.
type RotaryEvent struct {
	Action   string         `json:"action"`
	Rotation RotaryRotation `json:"rotation"`
}

// RotaryReport is a dial event and when it happened.
type RotaryReport struct {
	Updated  string         `json:"updated"`
	Action   string         `json:"action"`
	Rotation RotaryRotation `json:"rotation"`
}

// RelativeRotary is a dial (the Hue tap dial switch's ring).
type RelativeRotary struct {
	ID             string     `json:"id"`
	IDv1           string     `json:"id_v1,omitempty"`
	Type           string     `json:"type"`
	Owner          Identifier `json:"owner"`
	RelativeRotary *struct {
		LastEvent    *RotaryEvent  `json:"last_event,omitempty"`
		RotaryReport *RotaryReport `json:"rotary_report,omitempty"`
	} `json:"relative_rotary,omitempty"`
}

// Event is the dial's last event; ok is false when none is known.
func (r RelativeRotary) Event() (RotaryEvent, bool) {
	switch {
	case r.RelativeRotary == nil:
		return RotaryEvent{}, false
	case r.RelativeRotary.RotaryReport != nil:
		rep := r.RelativeRotary.RotaryReport
		return RotaryEvent{Action: rep.Action, Rotation: rep.Rotation}, true
	case r.RelativeRotary.LastEvent != nil:
		return *r.RelativeRotary.LastEvent, true
	}
	return RotaryEvent{}, false
}

// DevicePower is a battery-powered device's battery.
type DevicePower struct {
	ID         string     `json:"id"`
	IDv1       string     `json:"id_v1,omitempty"`
	Type       string     `json:"type"`
	Owner      Identifier `json:"owner"`
	PowerState struct {
		BatteryState string `json:"battery_state,omitempty"` // normal, low, critical
		BatteryLevel *int   `json:"battery_level,omitempty"` // percent
	} `json:"power_state"`
}

// ZigbeeChannel is the bridge's Zigbee channel (channel_11, channel_15,
// channel_20, channel_25) and the state of a change to it.
type ZigbeeChannel struct {
	Value  string `json:"value,omitempty"`
	Status string `json:"status,omitempty"`
}

// ZigbeeConnectivity is a device's Zigbee link: status connected,
// disconnected, connectivity_issue, unidirectional_incoming or
// pending_discovery.
type ZigbeeConnectivity struct {
	ID            string         `json:"id"`
	IDv1          string         `json:"id_v1,omitempty"`
	Type          string         `json:"type"`
	Owner         Identifier     `json:"owner"`
	Status        string         `json:"status"`
	MACAddress    string         `json:"mac_address,omitempty"`
	Channel       *ZigbeeChannel `json:"channel,omitempty"`
	ExtendedPanID string         `json:"extended_pan_id,omitempty"`
}

// ZGPConnectivity is the link of a Zigbee Green Power device (the Hue
// tap switch, friends of Hue switches).
type ZGPConnectivity struct {
	ID       string     `json:"id"`
	IDv1     string     `json:"id_v1,omitempty"`
	Type     string     `json:"type"`
	Owner    Identifier `json:"owner"`
	Status   string     `json:"status"`
	SourceID string     `json:"source_id,omitempty"`
}

// Bridge is the bridge service: its bridge ID and time zone.
type Bridge struct {
	ID       string     `json:"id"`
	IDv1     string     `json:"id_v1,omitempty"`
	Type     string     `json:"type"`
	Owner    Identifier `json:"owner"`
	BridgeID string     `json:"bridge_id"`
	TimeZone struct {
		TimeZone string `json:"time_zone"`
	} `json:"time_zone"`
}

// BridgeHome is the whole home: its rooms and devices outside rooms
// (Children) and its grouped light and other grouped services.
type BridgeHome struct {
	ID       string       `json:"id"`
	IDv1     string       `json:"id_v1,omitempty"`
	Type     string       `json:"type"`
	Children []Identifier `json:"children"`
	Services []Identifier `json:"services"`
}

// ContactReport is a contact sensor's state (contact, no_contact) and
// when it changed.
type ContactReport struct {
	Changed string `json:"changed"`
	State   string `json:"state"`
}

// Contact is a door or window contact service.
type Contact struct {
	ID            string         `json:"id"`
	IDv1          string         `json:"id_v1,omitempty"`
	Type          string         `json:"type"`
	Owner         Identifier     `json:"owner"`
	Enabled       bool           `json:"enabled"`
	ContactReport *ContactReport `json:"contact_report,omitempty"`
}

// Open tells whether the contact is open; ok is false without a report.
func (c Contact) Open() (open, ok bool) {
	if c.ContactReport == nil {
		return false, false
	}
	return c.ContactReport.State == "no_contact", true
}

// TamperReport is a tamper state (tampered, not_tampered), its source
// (battery_door, …) and when it changed.
type TamperReport struct {
	Changed string `json:"changed"`
	Source  string `json:"source"`
	State   string `json:"state"`
}

// Tamper is a device's tamper detection.
type Tamper struct {
	ID            string         `json:"id"`
	IDv1          string         `json:"id_v1,omitempty"`
	Type          string         `json:"type"`
	Owner         Identifier     `json:"owner"`
	TamperReports []TamperReport `json:"tamper_reports"`
}

// Segment is a range of a light's segments.
type Segment struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// EntertainmentService is a device's entertainment service: whether it
// renders a stream (a light) or relays it (a proxy), and its segments.
type EntertainmentService struct {
	ID                string      `json:"id"`
	IDv1              string      `json:"id_v1,omitempty"`
	Type              string      `json:"type"`
	Owner             Identifier  `json:"owner"`
	Renderer          bool        `json:"renderer"`
	RendererReference *Identifier `json:"renderer_reference,omitempty"`
	Proxy             bool        `json:"proxy"`
	Equalizer         *bool       `json:"equalizer,omitempty"`
	MaxStreams        *int        `json:"max_streams,omitempty"`
	Segments          *struct {
		Configurable bool      `json:"configurable"`
		MaxSegments  int       `json:"max_segments"`
		Segments     []Segment `json:"segments"`
	} `json:"segments,omitempty"`
}

// TimeslotTime is a time of day.
type TimeslotTime struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
	Second int `json:"second"`
}

// SmartSceneTimeslot is when a smart scene switches to a scene: at a
// time (Kind "time") or at sunset ("sunset").
type SmartSceneTimeslot struct {
	StartTime struct {
		Kind string        `json:"kind"`
		Time *TimeslotTime `json:"time,omitempty"`
	} `json:"start_time"`
	Target Identifier `json:"target"`
}

// SmartScene is a scene that changes with the time of day, per weekday.
type SmartScene struct {
	ID            string     `json:"id"`
	IDv1          string     `json:"id_v1,omitempty"`
	Type          string     `json:"type"`
	Metadata      Metadata   `json:"metadata"`
	Group         Identifier `json:"group"`
	WeekTimeslots []struct {
		Timeslots  []SmartSceneTimeslot `json:"timeslots"`
		Recurrence []string             `json:"recurrence"` // monday … sunday
	} `json:"week_timeslots"`
	TransitionDuration *int   `json:"transition_duration,omitempty"`
	State              string `json:"state"` // active, inactive
	ActiveTimeslot     *struct {
		TimeslotID int    `json:"timeslot_id"`
		Weekday    string `json:"weekday"`
	} `json:"active_timeslot,omitempty"`
}

// BehaviorScript is an automation template of the bridge; a
// behavior_instance runs one with a configuration in the shape of
// ConfigurationSchema.
type BehaviorScript struct {
	ID                  string         `json:"id"`
	IDv1                string         `json:"id_v1,omitempty"`
	Type                string         `json:"type"`
	Description         string         `json:"description"`
	ConfigurationSchema map[string]any `json:"configuration_schema"`
	TriggerSchema       map[string]any `json:"trigger_schema"`
	StateSchema         map[string]any `json:"state_schema"`
	Version             string         `json:"version"`
	Metadata            struct {
		Name     string `json:"name,omitempty"`
		Category string `json:"category,omitempty"` // automation, entertainment, accessory, other
	} `json:"metadata"`
	SupportedFeatures  []string `json:"supported_features,omitempty"`
	MaxNumberInstances *int     `json:"max_number_instances,omitempty"`
}

// DeviceSoftwareUpdate is a device's software update state (no_update,
// update_pending, ready_to_install, installing) and its problems.
type DeviceSoftwareUpdate struct {
	ID       string     `json:"id"`
	IDv1     string     `json:"id_v1,omitempty"`
	Type     string     `json:"type"`
	Owner    Identifier `json:"owner"`
	State    string     `json:"state"`
	Problems []string   `json:"problems"`
}

// Geolocation is whether the bridge knows its location, and today's sun.
type Geolocation struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	IsConfigured bool   `json:"is_configured"`
	SunToday     *struct {
		SunsetTime string `json:"sunset_time"`
		DayType    string `json:"day_type"`
	} `json:"sun_today,omitempty"`
}

// Homekit is the bridge's HomeKit pairing state (paired, pairing,
// unpaired).
type Homekit struct {
	ID     string `json:"id"`
	IDv1   string `json:"id_v1,omitempty"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// Matter is the bridge's Matter state.
type Matter struct {
	ID                    string `json:"id"`
	IDv1                  string `json:"id_v1,omitempty"`
	Type                  string `json:"type"`
	MaxFabrics            int    `json:"max_fabrics"`
	HasQRCode             bool   `json:"has_qr_code"`
	SoftwareVersionString string `json:"software_version_string,omitempty"`
}

// GeofenceClient is a phone the bridge uses for presence (at home or
// away).
type GeofenceClient struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}
