// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/cB-Sigma/hue"
)

// Pair with a bridge, list its lights and switch one off. It needs a
// bridge on the LAN (and the Hue root CA in hueRootCA), so it is not run;
// github.com/cB-Sigma/hue-sim runs the same steps against a simulated
// bridge.
func Example() {
	ctx := context.Background()
	const address, bridgeID = "192.168.1.20", "001788fffe4a1b2c"
	var hueRootCA []byte // PEM of the Hue bridge root CA, which this module does not ship

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(hueRootCA)
	check := hue.TLSCheck{BridgeID: func() string { return bridgeID }, Roots: roots}

	c := hue.NewClient(hue.Config{Address: address, TLS: check})
	res, err := c.Pair(ctx, "example#demo")
	if errors.Is(err, hue.ErrLinkButton) {
		fmt.Println("press the link button and try again")
		return
	} else if err != nil {
		fmt.Println(err)
		return
	}

	c = hue.NewClient(hue.Config{Address: address, AppKey: res.AppKey, TLS: check})
	lights, err := c.List(ctx, "light")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, l := range lights {
		on, _ := l.Bool("on", "on")
		fmt.Println(l.Str("metadata", "name"), "on:", on)
	}
	if len(lights) > 0 {
		err := c.Put(ctx, "light", lights[0].ID(), map[string]any{"on": map[string]any{"on": false}})
		fmt.Println("switched off:", err == nil)
	}
}

func ExampleReadEvents() {
	stream := ": hi\n\n" +
		`data: [{"type":"update","id":"e1","creationtime":"2026-10-06T08:00:00Z",` +
		`"data":[{"id":"b1","type":"button","owner":{"rid":"d1","rtype":"device"},"button":{"button_report":{"event":"short_release"}}}]}]` + "\n\n"
	err := hue.ReadEvents(strings.NewReader(stream), func(evs []hue.Event) {
		for _, e := range evs {
			for _, r := range e.Data {
				fmt.Println(e.Type, r.Type(), r.Owner(), hue.LastButtonEvent(r))
			}
		}
	}, nil)
	fmt.Println(err)
	// Output:
	// update button d1 short_release
	// the bridge closed the stream
}

func ExampleLuxFromLightLevel() {
	fmt.Println(hue.LuxFromLightLevel(23980))
	// Output: 250
}
