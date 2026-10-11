// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package interop

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cB-Sigma/hue"
)

// The peer: diyHue's published image (the "latest" tag of 2026-10-05,
// built from diyHue commit 3ee0461), pinned by digest.
const image = "diyhue/core@sha256:5501ec39c4bf73e8f5b750988151b05e1fd4a0ef1602d8c98a9eaf80c0674e75"

// A made-up MAC; diyHue derives the bridge ID from it.
const (
	mac      = "02:42:ac:11:00:aa"
	bridgeID = "0242acfffe1100aa"
)

type peer struct {
	name       string
	httpAddr   string // 127.0.0.1:port of diyHue's HTTP port
	httpsAddr  string // 127.0.0.1:port of its HTTPS port
	certPEM    []byte // diyHue's certificate, read out of the container
	beforeLink error  // Pair before the link button
	pair       hue.PairResult
	pairErr    error
}

var (
	once    sync.Once
	the     peer
	own     bool
	peerErr error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if own {
		_ = exec.Command("docker", "container", "rm", "-f", the.name).Run()
	}
	os.Exit(code)
}

func docker(args ...string) (string, error) {
	var out, errb bytes.Buffer
	c := exec.Command("docker", args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %v: %s", args[0], err, errb.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// pressLinkButton opens diyHue's 30 s pairing window through its v1
// configuration, from the container's own loopback address.
func pressLinkButton(name string) error {
	_, err := docker("exec", name, "python3", "-c",
		`import urllib.request as u; u.urlopen(u.Request("http://127.0.0.1/api/local/config", data=b'{"linkbutton": true}', method="PUT")).read()`)
	return err
}

// check is the certificate check the tests use for diyHue: the bridge ID
// as common name, and the chain verified against diyHue's own
// self-signed certificate.
func check(t *testing.T, p peer) hue.TLSCheck {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(p.certPEM) {
		t.Fatal("no certificate read from the container")
	}
	return hue.TLSCheck{BridgeID: func() string { return bridgeID }, Roots: roots}
}

func need(t *testing.T) peer {
	t.Helper()
	if os.Getenv("SIGMA_HUE_INTEROP") != "1" {
		t.Skip("runs diyHue in Docker; set SIGMA_HUE_INTEROP=1 to run")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not on PATH")
	}
	once.Do(func() { peerErr = start() })
	if peerErr != nil {
		t.Fatal(peerErr)
	}
	return the
}

func start() error {
	the.name = fmt.Sprintf("sigma-hue-diyhue-%d", os.Getpid())
	if _, err := docker("run", "-d", "--name", the.name, "-e", "MAC="+mac, "-e", "IP=127.0.0.1",
		"-p", "127.0.0.1::80", "-p", "127.0.0.1::443", image); err != nil {
		return err
	}
	own = true
	var err error
	if the.httpAddr, err = docker("port", the.name, "80/tcp"); err != nil {
		return err
	}
	if the.httpsAddr, err = docker("port", the.name, "443/tcp"); err != nil {
		return err
	}
	the.httpAddr = strings.Split(the.httpAddr, "\n")[0]
	the.httpsAddr = strings.Split(the.httpsAddr, "\n")[0]
	// Wait for the public configuration.
	hc := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	for {
		cfg, err := hue.ReadV1Config(context.Background(), hc, the.httpAddr)
		if err == nil && cfg.BridgeID != "" {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := docker("logs", "--tail", "40", the.name)
			return fmt.Errorf("diyHue did not answer: %v\n%s", err, logs)
		}
		time.Sleep(time.Second)
	}
	pemText, err := docker("exec", the.name, "cat", "/opt/hue-emulator/config/cert.pem")
	if err != nil {
		return err
	}
	the.certPEM = []byte(pemText)
	// Pairing: refused before the link button, then a key.
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(the.certPEM)
	c := hue.NewClient(hue.Config{Address: the.httpsAddr, Timeout: 5 * time.Second,
		TLS: hue.TLSCheck{BridgeID: func() string { return bridgeID }, Roots: roots}})
	_, the.beforeLink = c.Pair(context.Background(), "sigma-interop#test")
	if err := pressLinkButton(the.name); err != nil {
		return err
	}
	the.pair, the.pairErr = c.Pair(context.Background(), "sigma-interop#test")
	if the.pairErr != nil {
		return nil // TestPairing reports it
	}
	// A light of diyHue's dummy protocol (no hardware behind it), added
	// through API v1 with the new key.
	body := `{"ip":"192.0.2.10","protocol":"dummy","config":{"lightModelID":"LCT015","lightName":"Interop lamp"}}`
	resp, err := http.Post("http://"+the.httpAddr+"/api/"+the.pair.AppKey+"/lights", "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func client(t *testing.T, p peer) *hue.Client {
	t.Helper()
	if p.pairErr != nil {
		t.Fatalf("no key: %v", p.pairErr)
	}
	return hue.NewClient(hue.Config{Address: p.httpsAddr, AppKey: p.pair.AppKey, TLS: check(t, p), Timeout: 10 * time.Second})
}

// lamp waits for the dummy light to appear in CLIP v2.
func lamp(t *testing.T, c *hue.Client) hue.Resource {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		lights, err := c.List(context.Background(), "light")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lights {
			if l.Str("metadata", "name") == "Interop lamp" {
				return l
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the light did not appear: %v", lights)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestPairing(t *testing.T) {
	p := need(t)
	if !errors.Is(p.beforeLink, hue.ErrLinkButton) {
		t.Errorf("before the link button: %v", p.beforeLink)
	}
	if p.pairErr != nil || p.pair.AppKey == "" || len(p.pair.ClientKey) != 32 {
		t.Fatalf("after the link button: %+v %v", p.pair, p.pairErr)
	}
	t.Logf("paired: application key of %d characters, client key of %d", len(p.pair.AppKey), len(p.pair.ClientKey))
}

func TestPublicConfig(t *testing.T) {
	p := need(t)
	ctx := context.Background()
	cfg, err := hue.ReadV1Config(ctx, &http.Client{Timeout: 5 * time.Second}, p.httpAddr)
	if err != nil || hue.NormaliseBridgeID(cfg.BridgeID) != bridgeID || cfg.ModelID != "BSB002" {
		t.Fatalf("v1 config %+v %v", cfg, err)
	}
	if hue.NeedsV1(cfg) {
		t.Errorf("diyHue (API %s) was taken for an API v1 bridge", cfg.APIVersion)
	}
	c := hue.NewClient(hue.Config{Address: p.httpsAddr, TLS: check(t, p)})
	cfg2, err := hue.ReadConfig(ctx, c.HTTPClient(), c.BaseURL())
	if err != nil || hue.NormaliseBridgeID(cfg2.BridgeID) != bridgeID {
		t.Fatalf("https config %+v %v", cfg2, err)
	}
	t.Logf("bridge %s, model %s, API %s, software %s", cfg.BridgeID, cfg.ModelID, cfg.APIVersion, cfg.SWVersion)
}

func TestCertificateCheck(t *testing.T) {
	p := need(t)
	ctx := context.Background()
	list := func(chk hue.TLSCheck) error {
		c := hue.NewClient(hue.Config{Address: p.httpsAddr, AppKey: p.pair.AppKey, TLS: chk, Timeout: 5 * time.Second})
		defer c.CloseIdleConnections()
		_, err := c.List(ctx, "bridge")
		return err
	}
	// The common name is the bridge ID and the chain verifies against the
	// certificate read from the container; the pin is told the fingerprint.
	var pinned string
	chk := check(t, p)
	chk.Pin = func(id, fp string) error {
		if id != bridgeID {
			return fmt.Errorf("pin for %s", id)
		}
		pinned = fp
		return nil
	}
	if err := list(chk); err != nil {
		t.Fatalf("verified: %v", err)
	}
	if len(pinned) != 64 {
		t.Errorf("pin %q", pinned)
	}
	// Without roots of its own the zero policy still holds: a bridge ID
	// that does not match is refused.
	if err := list(hue.TLSCheck{BridgeID: func() string { return "001788fffe000000" }}); err == nil || !strings.Contains(err.Error(), "is not the bridge ID") {
		t.Errorf("wrong bridge ID: %v", err)
	}
	// Against other roots (as the Hue root CA would be for diyHue), the
	// chain problem goes to Unverified, which may accept it.
	other := x509.NewCertPool()
	var problems []string
	if err := list(hue.TLSCheck{BridgeID: func() string { return bridgeID }, Roots: other,
		Unverified: func(_, _ string, pr []string) error { problems = pr; return nil }}); err != nil {
		t.Fatalf("accepted by Unverified: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "trusted roots") {
		t.Errorf("problems %q", problems)
	}
}

func TestResources(t *testing.T) {
	p := need(t)
	c := client(t, p)
	ctx := context.Background()
	br, err := c.List(ctx, "bridge")
	if err != nil || len(br) != 1 || hue.NormaliseBridgeID(br[0].Str("bridge_id")) != bridgeID {
		t.Fatalf("bridge %v %v", br, err)
	}
	l := lamp(t, c)
	if l.Type() != "light" || l.Owner() == "" {
		t.Errorf("light %v", l)
	}
	devs, err := c.List(ctx, "device")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range devs {
		if d.ID() == l.Owner() {
			found = true
			for _, s := range d.Refs("services") {
				if s.RType == "light" && s.RID != l.ID() {
					t.Errorf("device %s lists light %s, not %s", d.ID(), s.RID, l.ID())
				}
			}
		}
	}
	if !found {
		t.Errorf("the light's owner %s is not among %d devices", l.Owner(), len(devs))
	}
	// A real bridge answers 404 for an unknown ID; diyHue's answer is
	// recorded, not judged.
	items, err := c.Do(ctx, http.MethodGet, hue.ResourcePath+"light/00000000-0000-0000-0000-000000000000", nil)
	t.Logf("unknown light: %d items, %v", len(items), err)
	bad := hue.NewClient(hue.Config{Address: p.httpsAddr, AppKey: "not-a-key", TLS: check(t, p)})
	if _, err := bad.List(ctx, "light"); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
}

func TestLightPutAndEventStream(t *testing.T) {
	p := need(t)
	c := client(t, p)
	l := lamp(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	body, err := c.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got := make(chan hue.Event, 64)
	go func() {
		_ = hue.ReadEvents(body, func(evs []hue.Event) {
			for _, e := range evs {
				got <- e
			}
		}, nil)
	}()
	if err := c.Put(ctx, "light", l.ID(), map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": 42.0}}); err != nil {
		t.Fatal(err)
	}
	// The change is in the light's state ...
	now := lamp(t, c)
	if on, _ := now.Bool("on", "on"); !on {
		t.Errorf("after PUT: %v", now)
	}
	if b, _ := now.Num("dimming", "brightness"); b < 41 || b > 43 {
		t.Errorf("brightness %v", b)
	}
	// ... and the event stream reports it, merged as a partial resource.
	// diyHue keeps an event for well under a second and its stream polls
	// for events on a timer, so a stream that has just opened can miss
	// one (seen on 2026-10-11); the whole change is repeated, with a new
	// brightness, up to five times.
	cache := l.Clone()
	for attempt := 1; attempt <= 5; attempt++ {
		want := float64(40 + attempt)
		if attempt > 1 {
			if err := c.Put(ctx, "light", l.ID(), map[string]any{"on": map[string]any{"on": true}, "dimming": map[string]any{"brightness": want}}); err != nil {
				t.Fatal(err)
			}
		} else {
			want = 42
		}
		deadline := time.After(4 * time.Second)
	wait:
		for {
			select {
			case e := <-got:
				for _, r := range e.Data {
					if e.Type == "update" && r.ID() == l.ID() {
						hue.Merge(cache, r)
						on, _ := cache.Bool("on", "on")
						b, _ := cache.Num("dimming", "brightness")
						if on && b > want-1 && b < want+1 {
							t.Logf("attempt %d: event %s %s: %v", attempt, e.Type, e.ID, r)
							return
						}
					}
				}
			case <-deadline:
				t.Logf("attempt %d: no event for the change within 4 s", attempt)
				break wait
			}
		}
	}
	t.Fatal("no update event for the light after five changes")
}

func TestAPIv1(t *testing.T) {
	p := need(t)
	c := client(t, p)
	_ = lamp(t, c)
	ctx := context.Background()
	v := hue.NewClient(hue.Config{Address: p.httpAddr})
	v.V1 = hue.NewV1Client(p.httpAddr, p.pair.AppKey, nil)
	lights, err := v.List(ctx, "light")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, l := range lights {
		if l.Str("metadata", "name") == "Interop lamp" {
			id = l.ID()
		}
	}
	if id == "" {
		t.Fatalf("v1 lights %v", lights)
	}
	if err := v.Put(ctx, "light", id, map[string]any{"on": map[string]any{"on": false}}); err != nil {
		t.Fatal(err)
	}
	raw, err := v.V1.Request(ctx, http.MethodGet, "/lights", nil)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]struct {
		Name  string `json:"name"`
		State struct {
			On bool `json:"on"`
		} `json:"state"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	for n, l := range all {
		if l.Name == "Interop lamp" && l.State.On {
			t.Errorf("light %s still on after the v1 change", n)
		}
	}
	if err := hue.NewV1Client(p.httpAddr, "not-a-key", nil).Refresh(ctx); !errors.Is(err, hue.ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
}
