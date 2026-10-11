// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package discover_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/hue/discover"
	"github.com/cB-Sigma/mdns"
)

// cloud is a fake discovery endpoint.
func cloud(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("accept %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCloud(t *testing.T) {
	url := cloud(t, 200, `[{"id":"001788FFFE4A1B2C","internalipaddress":"192.168.1.20","port":443},
		{"id":"ecb5fafffe0a0b0d","internalipaddress":"192.168.1.21"},{"id":"bad","internalipaddress":"not an address"}]`)
	got, err := discover.Cloud(context.Background(), nil, url)
	if err != nil {
		t.Fatal(err)
	}
	want := []discover.Bridge{
		{ID: "001788fffe4a1b2c", Host: "192.168.1.20", Port: 443, Sources: []discover.Source{discover.SourceCloud}},
		{ID: "ecb5fafffe0a0b0d", Host: "192.168.1.21", Port: 443, Sources: []discover.Source{discover.SourceCloud}},
	}
	if !reflect.DeepEqual(got, want) || got[0].Addr() != "192.168.1.20:443" {
		t.Errorf("got %+v", got)
	}
	if _, err := discover.Cloud(context.Background(), nil, cloud(t, 429, `rate limited`)); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("429: %v", err)
	}
	if _, err := discover.Cloud(context.Background(), nil, cloud(t, 200, `{}`)); err == nil {
		t.Error("an object: no error")
	}
	if got, err := discover.Cloud(context.Background(), nil, cloud(t, 200, `[]`)); err != nil || len(got) != 0 {
		t.Errorf("empty %v %v", got, err)
	}
}

func TestMerge(t *testing.T) {
	m := []discover.Bridge{{ID: "001788fffe4a1b2c", Host: "192.168.1.20", Port: 443, Name: "Hue Bridge - 4A1B2C", ModelID: "BSB002", Sources: []discover.Source{discover.SourceMDNS}}}
	c := []discover.Bridge{{ID: "001788fffe4a1b2c", Host: "192.168.1.20", Port: 443, Sources: []discover.Source{discover.SourceCloud}},
		{Host: "192.168.1.30", Port: 443, Sources: []discover.Source{discover.SourceCloud}}}
	got := discover.Merge(c, m)
	if len(got) != 2 || got[1].Name != "Hue Bridge - 4A1B2C" || got[1].ModelID != "BSB002" || len(got[1].Sources) != 2 || got[0].ID != "" {
		t.Errorf("merged %+v", got)
	}
}

func TestAll(t *testing.T) {
	url := cloud(t, 200, `[{"id":"001788fffe4a1b2c","internalipaddress":"192.168.1.20","port":443}]`)
	got, err := discover.All(context.Background(), discover.Options{NoMDNS: true, CloudURL: url})
	if err != nil || len(got) != 1 {
		t.Fatalf("all %v %v", got, err)
	}
	if _, err := discover.All(context.Background(), discover.Options{NoMDNS: true, CloudURL: cloud(t, 500, "")}); err == nil {
		t.Error("the only search failed: no error")
	}
	if _, err := discover.All(context.Background(), discover.Options{NoMDNS: true, NoCloud: true}); err == nil {
		t.Error("nothing to run: no error")
	}
}

// An announcement made with Announcement is found by MDNS, over the
// multicast interface the mdns module picks for tests (skipped where
// multicast does not come back to this host).
func TestMDNS(t *testing.T) {
	port := 25353 + os.Getpid()%1000
	ifi, err := mdns.TestInterface(port)
	if err != nil {
		t.Skip("no multicast loopback here:", err)
	}
	opts := mdns.Options{Interfaces: []net.Interface{*ifi}, Port: port, Loopback: true}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	svc := discover.Announcement("Hue Bridge - 4A1B2C", "00:17:88:FF:FE:4A:1B:2C", "BSB002", 8443)
	svc.IPs = []net.IP{net.IPv4(127, 0, 0, 1)}
	a, err := mdns.Announce(ctx, svc, discover.Service, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	opts.Wait = 3 * time.Second
	var got []discover.Bridge
	for i := 0; i < 3 && len(got) == 0; i++ {
		if got, err = discover.MDNS(ctx, opts); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 {
		t.Fatalf("found %+v", got)
	}
	b := got[0]
	if b.ID != "001788fffe4a1b2c" || b.ModelID != "BSB002" || b.Port != 8443 || b.Host != "127.0.0.1" || b.Name != "Hue Bridge - 4A1B2C" {
		t.Errorf("bridge %+v", b)
	}
	t.Logf("found %s at %s", b.ID, b.Addr())
}
