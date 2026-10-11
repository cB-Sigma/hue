// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package entertainment_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cB-Sigma/hue"
	"github.com/cB-Sigma/hue/entertainment"
)

// Open starts an entertainment area and streams a colour to its first
// channel for a second. It needs a bridge (or hue-sim's simulator with
// ServeEntertainment), so it compiles but does not run.
func Example() {
	ctx := context.Background()
	c := hue.NewClient(hue.Config{Address: "192.168.1.20", AppKey: "application key from pairing",
		TLS: hue.TLSCheck{BridgeID: func() string { return "001788fffe4a1b2c" }}})
	areas, err := hue.ListAs[hue.EntertainmentConfiguration](ctx, c, "entertainment_configuration")
	if err != nil || len(areas) == 0 {
		return
	}
	s, err := entertainment.Open(ctx, c, areas[0].ID, "client key from pairing", entertainment.Options{Repeat: 40 * time.Millisecond})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = s.Close(ctx) }()
	for i := 0; i < 50; i++ {
		_ = s.Send(entertainment.RGBChannel(uint8(areas[0].Channels[0].ChannelID), 1, 0.4, 0))
		time.Sleep(20 * time.Millisecond)
	}
}

// A frame in the XY colour space, as bytes.
func ExampleFrame_MarshalBinary() {
	b, _ := entertainment.Frame{ConfigID: "1a8d99cc-967b-44f2-9202-43f976c0fa6b", Space: entertainment.XY,
		Channels: []entertainment.Channel{entertainment.XYChannel(0, 0.5, 0.4, 1)}}.MarshalBinary()
	fmt.Println(len(b), string(b[:9]), b[9], b[14])
	// Output: 59 HueStream 2 1
}
