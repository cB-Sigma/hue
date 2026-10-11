// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package entertainment

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// A HueStream frame (API v2), as the bridge reads it from the DTLS
// stream, laid out in our own words:
//
//	bytes 0-8    the ASCII text "HueStream"
//	bytes 9-10   the format version, 2 and 0
//	byte  11     a sequence number (the bridge ignores it)
//	bytes 12-13  zero
//	byte  14     the colour space: 0 RGB, 1 CIE xy with brightness
//	byte  15     zero
//	bytes 16-51  the entertainment configuration's ID, 36 ASCII characters
//	then 7 bytes per channel: the channel ID and three 16-bit big-endian
//	values (red, green, blue; or x, y, brightness), at most 20 channels.
//
// Version 1 frames (16-byte header, 9 bytes per light, addressed by v1
// light number) are read by ParseV1.

// Magic starts every frame.
const Magic = "HueStream"

// HeaderLen is the length of a version 2 frame's header.
const HeaderLen = 52

// MaxChannels is the most channels one frame carries.
const MaxChannels = 20

// ColourSpace says how a frame's channel values are meant.
type ColourSpace byte

const (
	// RGB values are red, green and blue, 0-65535 each.
	RGB ColourSpace = 0
	// XY values are the CIE 1931 x and y (0-65535 for 0-1) and the
	// brightness (0-65535 for 0-100 %).
	XY ColourSpace = 1
)

// String names the colour space.
func (s ColourSpace) String() string {
	switch s {
	case RGB:
		return "RGB"
	case XY:
		return "XY"
	}
	return fmt.Sprintf("colour space %d", byte(s))
}

// Channel is one channel's colour in a frame.
type Channel struct {
	// ID is the channel_id of the entertainment configuration.
	ID uint8
	// A, B and C are red, green and blue, or x, y and brightness.
	A, B, C uint16
}

// Frame is one HueStream message.
type Frame struct {
	// ConfigID is the entertainment configuration's ID (a UUID).
	ConfigID string
	// Space is the colour space of the channel values.
	Space ColourSpace
	// Seq is the sequence number.
	Seq uint8
	// Channels are the colours.
	Channels []Channel
}

// Errors of Parse and MarshalBinary.
var (
	ErrNotHueStream = errors.New("not a HueStream frame")
	ErrVersion      = errors.New("unsupported HueStream version")
)

// MarshalBinary encodes a version 2 frame.
func (f Frame) MarshalBinary() ([]byte, error) {
	if len(f.ConfigID) != 36 {
		return nil, fmt.Errorf("the configuration ID %q is not 36 characters", f.ConfigID)
	}
	if len(f.Channels) > MaxChannels {
		return nil, fmt.Errorf("%d channels; a frame carries at most %d", len(f.Channels), MaxChannels)
	}
	if f.Space != RGB && f.Space != XY {
		return nil, fmt.Errorf("unknown %v", f.Space)
	}
	b := make([]byte, 0, HeaderLen+7*len(f.Channels))
	b = append(b, Magic...)
	b = append(b, 2, 0, f.Seq, 0, 0, byte(f.Space), 0)
	b = append(b, f.ConfigID...)
	for _, c := range f.Channels {
		b = append(b, c.ID)
		b = binary.BigEndian.AppendUint16(b, c.A)
		b = binary.BigEndian.AppendUint16(b, c.B)
		b = binary.BigEndian.AppendUint16(b, c.C)
	}
	return b, nil
}

// Parse decodes a version 2 frame. Trailing bytes shorter than a channel
// are an error.
func Parse(b []byte) (Frame, error) {
	if len(b) < 16 || !bytes.Equal(b[:9], []byte(Magic)) {
		return Frame{}, ErrNotHueStream
	}
	if b[9] != 2 {
		return Frame{}, fmt.Errorf("%w %d.%d (want 2.0)", ErrVersion, b[9], b[10])
	}
	if len(b) < HeaderLen {
		return Frame{}, fmt.Errorf("a version 2 frame of %d bytes (the header is %d)", len(b), HeaderLen)
	}
	f := Frame{Seq: b[11], Space: ColourSpace(b[14]), ConfigID: string(b[16:52])}
	if f.Space != RGB && f.Space != XY {
		return Frame{}, fmt.Errorf("unknown %v", f.Space)
	}
	rest := b[HeaderLen:]
	if len(rest)%7 != 0 {
		return Frame{}, fmt.Errorf("%d channel bytes are not a multiple of 7", len(rest))
	}
	if len(rest)/7 > MaxChannels {
		return Frame{}, fmt.Errorf("%d channels; a frame carries at most %d", len(rest)/7, MaxChannels)
	}
	for i := 0; i < len(rest); i += 7 {
		f.Channels = append(f.Channels, Channel{ID: rest[i], A: binary.BigEndian.Uint16(rest[i+1:]),
			B: binary.BigEndian.Uint16(rest[i+3:]), C: binary.BigEndian.Uint16(rest[i+5:])})
	}
	return f, nil
}

// V1Light is one light's colour in a version 1 frame.
type V1Light struct {
	// Type is 0 for a light, 1 for a gradient segment.
	Type uint8
	// ID is the v1 light number (or segment).
	ID uint16
	// A, B and C are as in Channel.
	A, B, C uint16
}

// ParseV1 decodes a version 1 frame (API v1 streaming, addressed by v1
// light numbers), for peers that still send it.
func ParseV1(b []byte) (ColourSpace, []V1Light, error) {
	if len(b) < 16 || !bytes.Equal(b[:9], []byte(Magic)) {
		return 0, nil, ErrNotHueStream
	}
	if b[9] != 1 {
		return 0, nil, fmt.Errorf("%w %d.%d (want 1.0)", ErrVersion, b[9], b[10])
	}
	rest := b[16:]
	if len(rest)%9 != 0 {
		return 0, nil, fmt.Errorf("%d light bytes are not a multiple of 9", len(rest))
	}
	var out []V1Light
	for i := 0; i < len(rest); i += 9 {
		out = append(out, V1Light{Type: rest[i], ID: binary.BigEndian.Uint16(rest[i+1:]), A: binary.BigEndian.Uint16(rest[i+3:]),
			B: binary.BigEndian.Uint16(rest[i+5:]), C: binary.BigEndian.Uint16(rest[i+7:])})
	}
	return ColourSpace(b[14]), out, nil
}

// unit maps 0-1 to 0-65535.
func unit(v float64) uint16 {
	return uint16(math.Round(math.Max(0, math.Min(1, v)) * 65535))
}

// RGBChannel is a channel in the RGB space, each component 0-1.
func RGBChannel(id uint8, r, g, b float64) Channel {
	return Channel{ID: id, A: unit(r), B: unit(g), C: unit(b)}
}

// XYChannel is a channel in the XY space: CIE x and y (0-1) and the
// brightness (0-1).
func XYChannel(id uint8, x, y, bri float64) Channel {
	return Channel{ID: id, A: unit(x), B: unit(y), C: unit(bri)}
}

// Values are a channel's three values as 0-1.
func (c Channel) Values() (a, b, cc float64) {
	return float64(c.A) / 65535, float64(c.B) / 65535, float64(c.C) / 65535
}

// RGBToXY converts an sRGB colour (each 0-1) to CIE 1931 x, y and a
// brightness (0-1, the relative luminance Y): gamma expansion and the
// sRGB to XYZ matrix of IEC 61966-2-1 (D65 white). Black is the D65
// white point with brightness 0.
func RGBToXY(r, g, b float64) (x, y, bri float64) {
	lin := func(c float64) float64 {
		c = math.Max(0, math.Min(1, c))
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	R, G, B := lin(r), lin(g), lin(b)
	X := 0.4124*R + 0.3576*G + 0.1805*B
	Y := 0.2126*R + 0.7152*G + 0.0722*B
	Z := 0.0193*R + 0.1192*G + 0.9505*B
	sum := X + Y + Z
	if sum == 0 {
		return 0.3127, 0.3290, 0
	}
	return X / sum, Y / sum, math.Min(1, Y)
}
