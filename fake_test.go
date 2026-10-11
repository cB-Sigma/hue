// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBridge is a small stand-in for a CLIP v2 bridge, enough for the
// client's own tests: one light, pairing, the public configuration, the
// event stream. The full simulator lives in github.com/cB-Sigma/hue-sim.
type fakeBridge struct {
	id  string
	srv *httptest.Server
	ca  []byte

	mu      sync.Mutex
	keys    map[string]bool
	link    bool
	light   map[string]any
	plug    map[string]any
	streams []chan string
	last    map[string]any // the last POST /api body
	created map[string]map[string]any
	posts   []map[string]any
	puts    []map[string]any
}

// The demo application key fakeBridge accepts.
const fakeKey = "fake-demo-app-key"

// newFake starts a fake bridge whose certificate has cn as common name,
// issued by a root of its own.
func newFake(t *testing.T, id, cn string) *fakeBridge {
	t.Helper()
	cert, ca := issue(t, cn)
	f := &fakeBridge{id: id, ca: ca, keys: map[string]bool{fakeKey: true}, created: map[string]map[string]any{},
		light: map[string]any{"id": "light-1", "type": "light", "owner": map[string]any{"rid": "dev-1", "rtype": "device"},
			"metadata": map[string]any{"name": "Sofa lamp"}, "on": map[string]any{"on": true},
			"color": map[string]any{"xy": map[string]any{"x": 0.3, "y": 0.3}}},
		plug: map[string]any{"id": "light-2", "type": "light", "owner": map[string]any{"rid": "dev-2", "rtype": "device"},
			"metadata": map[string]any{"name": "Plug"}, "on": map[string]any{"on": false}},
	}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	f.srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	f.srv.StartTLS()
	t.Cleanup(f.close)
	return f
}

func (f *fakeBridge) addr() string { return f.srv.Listener.Addr().String() }

func (f *fakeBridge) close() {
	f.dropStreams()
	f.srv.Close()
}

func (f *fakeBridge) roots(t *testing.T) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(f.ca) {
		t.Fatal("no CA")
	}
	return p
}

func (f *fakeBridge) pressLinkButton() { f.mu.Lock(); f.link = true; f.mu.Unlock() }

func (f *fakeBridge) removeKey(k string) { f.mu.Lock(); delete(f.keys, k); f.mu.Unlock() }

func (f *fakeBridge) streamCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.streams) }

func (f *fakeBridge) dropStreams() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.streams {
		close(ch)
	}
	f.streams = nil
}

// emit sends one SSE message with an update event to every open stream.
func (f *fakeBridge) emit(data map[string]any) {
	ev := []map[string]any{{"type": "update", "id": "ev-1", "creationtime": time.Now().UTC().Format(time.RFC3339), "data": []any{data}}}
	raw, _ := json.Marshal(ev)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.streams {
		ch <- "id: 1:0\ndata: " + string(raw) + "\n\n"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func clipErr(w http.ResponseWriter, status int, desc string) {
	writeJSON(w, status, map[string]any{"errors": []any{map[string]any{"description": desc}}, "data": []any{}})
}

func (f *fakeBridge) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api":
		f.mu.Lock()
		defer f.mu.Unlock()
		f.last = body
		switch dt, _ := body["devicetype"].(string); {
		case dt == "":
			writeJSON(w, http.StatusOK, []any{map[string]any{"error": map[string]any{"type": 5, "description": "invalid/missing parameters in body"}}})
		case !f.link:
			writeJSON(w, http.StatusOK, []any{map[string]any{"error": map[string]any{"type": 101, "description": "link button not pressed"}}})
		default:
			f.link = false
			key := fmt.Sprintf("fake-paired-key-%d", len(f.keys))
			f.keys[key] = true
			writeJSON(w, http.StatusOK, []any{map[string]any{"success": map[string]any{"username": key, "clientkey": "00112233445566778899AABBCCDDEEFF"}}})
		}
		return
	case r.Method == http.MethodGet && r.URL.Path == "/api/0/config":
		writeJSON(w, http.StatusOK, map[string]any{"name": "Fake bridge", "bridgeid": strings.ToUpper(f.id), "modelid": "BSB002", "apiversion": "1.60.0", "swversion": "1960000000"})
		return
	}
	f.mu.Lock()
	ok := f.keys[r.Header.Get("hue-application-key")]
	f.mu.Unlock()
	if !ok {
		clipErr(w, http.StatusForbidden, "unauthorized user")
		return
	}
	switch {
	case r.URL.Path == "/eventstream/clip/v2":
		f.stream(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/clip/v2/resource/light":
		f.mu.Lock()
		data := []any{f.light, f.plug}
		raw, _ := json.Marshal(map[string]any{"errors": []any{}, "data": data})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	case r.URL.Path == "/auth/v1" && r.Method == http.MethodGet:
		w.Header().Set("hue-application-id", "app-id-of-"+r.Header.Get("hue-application-key"))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/"):
		rtype := strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.posts = append(f.posts, map[string]any{"rtype": rtype, "body": body})
		if rtype == "room" {
			// A room's children must be known devices.
			ch, _ := body["children"].([]any)
			for _, c := range ch {
				if cm, _ := c.(map[string]any); cm["rid"] != "dev-1" && cm["rid"] != "dev-2" {
					clipErr(w, http.StatusBadRequest, fmt.Sprintf("invalid child %v", cm["rid"]))
					return
				}
			}
		}
		id := fmt.Sprintf("new-%s-%d", rtype, len(f.posts))
		stored := map[string]any{"id": id, "type": rtype}
		for k, v := range body {
			stored[k] = v
		}
		f.created[id] = stored
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}, "data": []any{map[string]any{"rid": id, "rtype": rtype}}})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(parts) != 2 || f.created[parts[1]] == nil {
			clipErr(w, http.StatusNotFound, "resource not found")
			return
		}
		delete(f.created, parts[1])
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}, "data": []any{map[string]any{"rid": parts[1], "rtype": parts[0]}}})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/") && !strings.HasPrefix(r.URL.Path, "/clip/v2/resource/light/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		x := f.created[parts[len(parts)-1]]
		if x == nil {
			clipErr(w, http.StatusNotFound, "resource not found")
			return
		}
		f.puts = append(f.puts, body)
		for k, v := range body {
			x[k] = v
		}
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}, "data": []any{map[string]any{"rid": x["id"], "rtype": parts[0]}}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(parts) == 2 {
			if x := f.created[parts[1]]; x != nil {
				writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}, "data": []any{x}})
				return
			}
		}
		clipErr(w, http.StatusNotFound, "resource not found")
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/clip/v2/resource/light/"):
		f.mu.Lock()
		defer f.mu.Unlock()
		var target map[string]any
		switch strings.TrimPrefix(r.URL.Path, "/clip/v2/resource/light/") {
		case "light-1":
			target = f.light
		case "light-2":
			target = f.plug
		default:
			clipErr(w, http.StatusNotFound, "resource not found")
			return
		}
		if _, ok := body["color"]; ok && target["color"] == nil {
			clipErr(w, http.StatusBadRequest, "device does not support color")
			return
		}
		for k, v := range body {
			target[k] = v
		}
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}, "data": []any{map[string]any{"rid": target["id"], "rtype": "light"}}})
	default:
		clipErr(w, http.StatusMethodNotAllowed, "method not available")
	}
}

func (f *fakeBridge) stream(w http.ResponseWriter, r *http.Request) {
	ch := make(chan string, 8)
	f.mu.Lock()
	f.streams = append(f.streams, ch)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": hi\n\n")
	fl := w.(http.Flusher)
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = io.WriteString(w, msg)
			fl.Flush()
		}
	}
}

// issue makes a root and a leaf for cn (IP SAN 127.0.0.1).
func issue(t *testing.T, cn string) (tls.Certificate, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caT := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake root-bridge"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caT, caT, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lt := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: cn},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, lt, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

// fakeV1 is a round bridge (API v1 over plain HTTP) with one colour light
// in a room, a group scene and a dimmer switch.
type fakeV1 struct {
	srv    *httptest.Server
	mu     sync.Mutex
	link   bool
	state  map[string]any
	puts   []string
	bodies []map[string]any
}

const fakeV1Key = "fake-v1-key"

func newFakeV1(t *testing.T) *fakeV1 {
	t.Helper()
	f := &fakeV1{state: map[string]any{
		"config": map[string]any{"name": "Philips hue", "bridgeid": "001788FFFE1A2B3C", "modelid": "BSB001", "apiversion": "1.16.0", "swversion": "01041302"},
		"lights": map[string]any{"1": map[string]any{"name": "Sofa lamp", "type": "Extended color light", "modelid": "LCT001", "productname": "Hue color lamp",
			"manufacturername": "Signify Netherlands B.V.", "uniqueid": "00:17:88:01:00:aa:bb:cc-0b", "config": map[string]any{"archetype": "sultanbulb"},
			"state": map[string]any{"on": false, "bri": 127.0, "reachable": true, "colormode": "xy", "xy": []any{0.45, 0.41}}}},
		"groups": map[string]any{"1": map[string]any{"name": "Living room", "type": "Room", "class": "Living room", "lights": []any{"1"},
			"state": map[string]any{"all_on": false, "any_on": false}, "action": map[string]any{"on": false}},
			"2": map[string]any{"name": "Wake-up lights", "type": "LightGroup", "lights": []any{"1"}, "state": map[string]any{"any_on": false}, "action": map[string]any{"on": false, "bri": 254.0}},
			"3": map[string]any{"name": "TV area", "type": "Entertainment", "class": "TV", "lights": []any{"1"}, "state": map[string]any{"any_on": false},
				"action": map[string]any{"on": false}, "stream": map[string]any{"active": false, "proxymode": "auto"},
				"locations": map[string]any{"1": []any{-0.5, 0.25, 0.0}}}},
		"scenes": map[string]any{"SceneDemo000001": map[string]any{"name": "Relax", "type": "GroupScene", "group": "1", "lights": []any{"1"}},
			"LightScene00001": map[string]any{"name": "Reading", "type": "LightScene", "lights": []any{"1"}}},
		"sensors": map[string]any{"7": map[string]any{"name": "Dimmer", "type": "ZLLSwitch", "modelid": "RWL021", "productname": "Hue dimmer switch",
			"uniqueid": "00:17:88:01:03:00:11:22-02-fc00", "state": map[string]any{"buttonevent": 1002.0, "lastupdated": "2026-10-05T09:00:00"},
			"config": map[string]any{"battery": 100.0}}},
	}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeV1) addr() string { return f.srv.Listener.Addr().String() }

func (f *fakeV1) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	v1err := func(typ int, desc string) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"error": map[string]any{"type": typ, "description": desc}}})
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodPost:
		if _, ok := body["generateclientkey"]; ok {
			v1err(2, "body contains invalid json")
		} else if !f.link {
			v1err(101, "link button not pressed")
		} else {
			f.link = false
			writeJSON(w, http.StatusOK, []any{map[string]any{"success": map[string]any{"username": fakeV1Key}}})
		}
	case len(parts) == 3 && parts[1] == "0" && parts[2] == "config":
		writeJSON(w, http.StatusOK, f.state["config"])
	case len(parts) < 2 || parts[1] != fakeV1Key:
		v1err(1, "unauthorized user")
	case r.Method == http.MethodGet && len(parts) == 2:
		writeJSON(w, http.StatusOK, f.state)
	case r.Method == http.MethodPost && len(parts) == 3 && (parts[2] == "groups" || parts[2] == "scenes"):
		f.puts = append(f.puts, "POST /"+parts[2])
		f.bodies = append(f.bodies, body)
		sec := f.state[parts[2]].(map[string]any)
		id := fmt.Sprint(len(sec) + 10)
		sec[id] = body
		writeJSON(w, http.StatusOK, []any{map[string]any{"success": map[string]any{"id": id}}})
	case r.Method == http.MethodDelete && len(parts) == 4:
		f.puts = append(f.puts, "DELETE /"+parts[2]+"/"+parts[3])
		sec, _ := f.state[parts[2]].(map[string]any)
		if sec[parts[3]] == nil {
			v1err(3, "resource not available")
			return
		}
		delete(sec, parts[3])
		writeJSON(w, http.StatusOK, []any{map[string]any{"success": "/" + parts[2] + "/" + parts[3] + " deleted"}})
	case r.Method == http.MethodPut && (len(parts) == 4 || len(parts) == 6):
		f.puts = append(f.puts, "/"+strings.Join(parts[2:], "/"))
		f.bodies = append(f.bodies, body)
		writeJSON(w, http.StatusOK, []any{map[string]any{"success": map[string]any{r.URL.Path: body}}})
	case r.Method == http.MethodPut && len(parts) == 5:
		f.puts = append(f.puts, "/"+strings.Join(parts[2:], "/"))
		if parts[2] == "lights" && parts[3] != "1" {
			v1err(3, "resource not available")
			return
		}
		if bri, ok := body["bri"].(float64); ok && (bri < 1 || bri > 254) {
			v1err(7, "invalid value")
			return
		}
		if parts[2] == "lights" {
			st := f.state["lights"].(map[string]any)["1"].(map[string]any)["state"].(map[string]any)
			for k, v := range body {
				st[k] = v
			}
		}
		writeJSON(w, http.StatusOK, []any{map[string]any{"success": map[string]any{r.URL.Path: body}}})
	default:
		v1err(4, "method not available")
	}
}
