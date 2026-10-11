// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package discover_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cB-Sigma/hue/discover"
	"github.com/cB-Sigma/mdns"
)

// All searches by multicast DNS and the public endpoint at once. It needs
// a network with bridges, so it compiles but does not run.
func ExampleAll() {
	found, err := discover.All(context.Background(), discover.Options{MDNS: mdns.Options{Wait: 3 * time.Second}})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, b := range found {
		fmt.Println(b.ID, b.Addr(), b.Sources)
	}
}
