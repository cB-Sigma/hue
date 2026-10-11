// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package discover finds Philips Hue bridges: by multicast DNS (the
// _hue._tcp service bridges announce, with their bridge ID and model in
// TXT), through the public discovery endpoint (discovery.meethue.com,
// which lists the bridges that reported from the caller's public
// address), by SSDP (the UPnP search the round bridge and old firmware
// answer), or several at once. It returns addresses and bridge IDs; check a
// found bridge with hue.ReadConfig or hue.ReadV1Config before trusting
// it, and connect with the bridge ID as the certificate's expected name.
//
// It is a module of its own, so that github.com/cB-Sigma/hue keeps no
// dependency beyond the standard library; it uses
// github.com/cB-Sigma/mdns.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cB-Sigma/hue"
	"github.com/cB-Sigma/mdns"
)

// Service is the DNS-SD service type Hue bridges announce.
const Service = "_hue._tcp"

// CloudURL is the public discovery endpoint.
const CloudURL = "https://discovery.meethue.com/"

// Source tells how a bridge was found.
type Source string

// The sources.
const (
	SourceMDNS  Source = "mdns"
	SourceCloud Source = "cloud"
)

// Bridge is a found bridge.
type Bridge struct {
	// ID is the bridge ID, normalised (lower case, no separators); empty
	// when the answer did not carry one.
	ID string
	// Host is the bridge's IP address (or host name when no address was
	// given).
	Host string
	// Port is the HTTPS port (443 when the answer gave none).
	Port int
	// Name is the mDNS instance name ("" from the endpoint).
	Name string
	// ModelID is the model the announcement named (BSB002, …).
	ModelID string
	// Location is the bridge's UPnP description URL from an SSDP answer
	// ("" from the other sources); HTTPAddr uses its host and port.
	Location string
	// Sources are where the bridge was found.
	Sources []Source
}

// Addr is host:port, for hue.Config.Address.
func (b Bridge) Addr() string { return net.JoinHostPort(b.Host, strconv.Itoa(b.Port)) }

// MDNS browses for _hue._tcp with the given mdns options (Wait bounds the
// search; mdns.DefaultWait when zero and ctx has no deadline).
func MDNS(ctx context.Context, opts mdns.Options) ([]Bridge, error) {
	found, err := mdns.Browse(ctx, Service, opts)
	if err != nil {
		return nil, err
	}
	out := make([]Bridge, 0, len(found))
	for _, s := range found {
		b := Bridge{ID: hue.NormaliseBridgeID(s.TXT["bridgeid"]), Name: s.Instance, ModelID: s.TXT["modelid"], Port: s.Port, Sources: []Source{SourceMDNS}}
		for _, ip := range s.IPs {
			if ip.To4() != nil {
				b.Host = ip.String()
				break
			}
		}
		if b.Host == "" && len(s.IPs) > 0 {
			b.Host = s.IPs[0].String()
		}
		if b.Host == "" {
			b.Host = strings.TrimSuffix(s.Host, ".")
		}
		if b.Port == 0 {
			b.Port = 443
		}
		out = append(out, b)
	}
	sortBridges(out)
	return out, nil
}

// Cloud asks the public discovery endpoint (url, CloudURL when empty)
// with hc (a client with a 10 s timeout when nil). The endpoint limits
// how often one address may ask; an answer other than 200 is an error
// naming the status.
func Cloud(ctx context.Context, hc *http.Client, url string) ([]Bridge, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	if url == "" {
		url = CloudURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the discovery endpoint answered HTTP %d", resp.StatusCode)
	}
	var items []struct {
		ID   string `json:"id"`
		IP   string `json:"internalipaddress"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&items); err != nil {
		return nil, fmt.Errorf("the discovery endpoint's answer is not a list of bridges: %w", err)
	}
	out := make([]Bridge, 0, len(items))
	for _, it := range items {
		if net.ParseIP(it.IP) == nil {
			continue
		}
		p := it.Port
		if p <= 0 || p > 65535 {
			p = 443
		}
		out = append(out, Bridge{ID: hue.NormaliseBridgeID(it.ID), Host: it.IP, Port: p, Sources: []Source{SourceCloud}})
	}
	sortBridges(out)
	return out, nil
}

// Options configure All.
type Options struct {
	// MDNS are the multicast DNS options; NoMDNS skips it.
	MDNS   mdns.Options
	NoMDNS bool
	// HTTPClient and CloudURL are Cloud's; NoCloud skips it.
	HTTPClient *http.Client
	CloudURL   string
	NoCloud    bool
	// SSDP adds an SSDP search (for round bridges and old firmware, which
	// mDNS does not find), with SSDPOptions.
	SSDP        bool
	SSDPOptions SSDPOptions
}

// All runs the searches at once (multicast DNS and the endpoint, and SSDP
// when asked) and merges what they found by bridge ID
// (or, without one, by address). It fails only when every search that
// ran failed; the errors are joined.
func All(ctx context.Context, o Options) ([]Bridge, error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		all  [][]Bridge
		errs []error
		ran  int
	)
	run := func(f func() ([]Bridge, error)) {
		ran++
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs, err := f()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			all = append(all, bs)
		}()
	}
	if !o.NoMDNS {
		run(func() ([]Bridge, error) { return MDNS(ctx, o.MDNS) })
	}
	if !o.NoCloud {
		run(func() ([]Bridge, error) { return Cloud(ctx, o.HTTPClient, o.CloudURL) })
	}
	if o.SSDP {
		run(func() ([]Bridge, error) { return SSDP(ctx, o.SSDPOptions) })
	}
	wg.Wait()
	if ran == 0 {
		return nil, errors.New("no search to run")
	}
	if len(errs) == ran {
		return nil, errors.Join(errs...)
	}
	return Merge(all...), nil
}

// Merge joins lists of found bridges: one entry per bridge ID (or per
// address for entries without one), mDNS details first, sources joined.
func Merge(lists ...[]Bridge) []Bridge {
	byKey := map[string]*Bridge{}
	var order []string
	for _, l := range lists {
		for _, b := range l {
			key := b.ID
			if key == "" {
				key = "addr:" + b.Addr()
			}
			cur := byKey[key]
			if cur == nil {
				c := b
				c.Sources = append([]Source(nil), b.Sources...)
				byKey[key] = &c
				order = append(order, key)
				continue
			}
			if cur.Name == "" {
				cur.Name = b.Name
			}
			if cur.ModelID == "" {
				cur.ModelID = b.ModelID
			}
			if cur.Location == "" {
				cur.Location = b.Location
			}
			for _, s := range b.Sources {
				if !hasSource(cur.Sources, s) {
					cur.Sources = append(cur.Sources, s)
				}
			}
		}
	}
	out := make([]Bridge, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sortBridges(out)
	return out
}

func hasSource(l []Source, s Source) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func sortBridges(l []Bridge) {
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].ID != l[j].ID {
			return l[i].ID < l[j].ID
		}
		return l[i].Addr() < l[j].Addr()
	})
}

// Announcement is the mDNS service a bridge announces: instance name,
// port and the TXT keys bridgeid and modelid. Simulators use it to
// announce themselves the way a bridge does.
func Announcement(name, bridgeID, modelID string, port int) mdns.Service {
	return mdns.Service{Instance: name, Host: "hue-" + hue.NormaliseBridgeID(bridgeID), Port: port,
		TXT: map[string]string{"bridgeid": hue.NormaliseBridgeID(bridgeID), "modelid": modelID}}
}
