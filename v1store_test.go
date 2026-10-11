// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// v1Store is a small stand-in for the API v1 sections without a CLIP v2
// form (rules, schedules, resource links, sensors, the light and sensor
// search, the configuration and its whitelist, capabilities), enough for
// the client's own tests. It stores what it is given and checks little;
// the full simulator is in github.com/cB-Sigma/hue-sim.
type v1Store struct {
	srv *httptest.Server

	mu    sync.Mutex
	state map[string]any
	log   []string // "METHOD /path" of the changes
	body  []any    // their bodies
}

const storeKey = "store-demo-key"

var storeSections = map[string]bool{"lights": true, "rules": true, "schedules": true, "resourcelinks": true, "sensors": true}

func newStoreState() map[string]any {
	return map[string]any{
		"config": map[string]any{"name": "Store bridge", "bridgeid": "001788FFFE5A6B7C", "modelid": "BSB002", "apiversion": "1.60.0", "swversion": "1960000000",
			"mac": "00:17:88:5a:6b:7c", "zigbeechannel": 15.0, "timezone": "Asia/Kolkata",
			"whitelist": map[string]any{storeKey: map[string]any{"name": "store#test", "create date": "2026-10-11T08:00:00", "last use date": "2026-10-11T09:00:00"},
				"other-key": map[string]any{"name": "other#app", "create date": "2026-10-01T08:00:00", "last use date": "2026-10-02T08:00:00"}},
			"swupdate2": map[string]any{"checkforupdate": false, "state": "noupdates", "lastchange": "2026-10-01T00:00:00",
				"bridge": map[string]any{"state": "noupdates", "lastinstall": "2026-09-30T03:00:00"}, "autoinstall": map[string]any{"on": true, "updatetime": "T03:00:00"}}},
		"lights":        map[string]any{"1": map[string]any{"name": "Hall lamp", "state": map[string]any{"on": false}}},
		"groups":        map[string]any{},
		"scenes":        map[string]any{},
		"rules":         map[string]any{"1": map[string]any{"name": "Old rule", "owner": storeKey, "status": "enabled", "timestriggered": 2.0, "lasttriggered": "none", "conditions": []any{map[string]any{"address": "/sensors/2/state/buttonevent", "operator": "eq", "value": 1002.0}}, "actions": []any{map[string]any{"address": "/groups/0/action", "method": "PUT", "body": map[string]any{"on": true}}}}},
		"schedules":     map[string]any{},
		"resourcelinks": map[string]any{},
		"sensors":       map[string]any{"2": map[string]any{"name": "Dimmer", "type": "ZLLSwitch", "modelid": "RWL021", "state": map[string]any{"buttonevent": 1002.0}, "config": map[string]any{"on": true, "battery": 90.0}}},
	}
}

// newV1Store starts the store on plain HTTP, or on HTTPS with a
// certificate for cn when cn is not empty (with its root's PEM).
func newV1Store(t *testing.T, cn string) (*v1Store, []byte) {
	t.Helper()
	s := &v1Store{state: newStoreState()}
	var ca []byte
	if cn == "" {
		s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	} else {
		var cert tls.Certificate
		cert, ca = issue(t, cn)
		s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
		s.srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		s.srv.StartTLS()
	}
	t.Cleanup(s.srv.Close)
	return s, ca
}

func (s *v1Store) addr() string { return s.srv.Listener.Addr().String() }

func pool(t *testing.T, pemData []byte) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(pemData) {
		t.Fatal("no CA")
	}
	return p
}

func (s *v1Store) changes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

func (s *v1Store) lastBody() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.body) == 0 {
		return nil
	}
	m, _ := s.body[len(s.body)-1].(map[string]any)
	return m
}

func (s *v1Store) get(path ...string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur any = s.state
	for _, p := range path {
		m, _ := cur.(map[string]any)
		cur = m[p]
	}
	return cur
}

func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeInto(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func (s *v1Store) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	v1err := func(typ int, addr, desc string) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"error": map[string]any{"type": typ, "address": addr, "description": desc}}})
	}
	if len(parts) < 2 || parts[0] != "api" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	wl := s.state["config"].(map[string]any)["whitelist"].(map[string]any)
	if _, ok := wl[parts[1]]; !ok {
		v1err(1, "/", "unauthorized user")
		return
	}
	p := parts[2:]
	if r.Method != http.MethodGet {
		s.log = append(s.log, r.Method+" /"+strings.Join(p, "/"))
		s.body = append(s.body, body)
	}
	ok := func(items ...any) { writeJSON(w, http.StatusOK, items) }
	switch {
	case r.Method == http.MethodGet && len(p) == 0:
		writeJSON(w, http.StatusOK, s.state)
	case r.Method == http.MethodGet && len(p) == 1 && p[0] == "capabilities":
		writeJSON(w, http.StatusOK, map[string]any{"lights": map[string]any{"available": 62, "total": 63},
			"sensors": map[string]any{"available": 249, "total": 250, "clip": map[string]any{"available": 249}, "zll": map[string]any{"available": 63}, "zgp": map[string]any{"available": 63}},
			"groups":  map[string]any{"available": 64}, "scenes": map[string]any{"available": 200, "lightstates": map[string]any{"available": 2048}},
			"rules":     map[string]any{"available": 199, "conditions": map[string]any{"available": 1500}, "actions": map[string]any{"available": 1000}},
			"schedules": map[string]any{"available": 100}, "resourcelinks": map[string]any{"available": 64}, "streaming": map[string]any{"available": 1, "total": 1, "channels": 10},
			"timezones": map[string]any{"values": []any{"Asia/Kolkata", "Europe/Amsterdam"}}})
	case r.Method == http.MethodGet && len(p) == 2 && p[1] == "new" && (p[0] == "lights" || p[0] == "sensors"):
		out := map[string]any{"lastscan": "none"}
		if found, ok := s.state["new-"+p[0]].(map[string]any); ok {
			for k, v := range found {
				out[k] = v
			}
		}
		writeJSON(w, http.StatusOK, out)
	case r.Method == http.MethodGet && (len(p) == 1 || len(p) == 2):
		sec, isSec := s.state[p[0]].(map[string]any)
		if !isSec {
			v1err(4, "/"+p[0], "method, GET, not available for resource, /"+p[0])
			return
		}
		if len(p) == 1 {
			writeJSON(w, http.StatusOK, sec)
			return
		}
		item, there := sec[p[1]]
		if !there {
			v1err(3, "/"+p[0]+"/"+p[1], "resource, /"+p[0]+"/"+p[1]+", not available")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case r.Method == http.MethodPost && len(p) == 1 && (p[0] == "lights" || p[0] == "sensors") && (len(body) == 0 || body["deviceid"] != nil):
		// A search: the store finds one device at once.
		name := "New lamp"
		if p[0] == "sensors" {
			name = "New switch"
		}
		s.state["new-"+p[0]] = map[string]any{"lastscan": "2026-10-11T10:00:00", "7": map[string]any{"name": name}}
		ok(map[string]any{"success": map[string]any{"/" + p[0]: "Searching for new devices"}})
	case r.Method == http.MethodPost && len(p) == 1 && storeSections[p[0]]:
		sec := s.state[p[0]].(map[string]any)
		id := ""
		for n := 1; ; n++ {
			if _, used := sec[strconv.Itoa(n)]; !used {
				id = strconv.Itoa(n)
				break
			}
		}
		item := map[string]any{}
		mergeInto(item, body)
		item["owner"] = parts[1]
		sec[id] = item
		ok(map[string]any{"success": map[string]any{"id": id}})
	case r.Method == http.MethodPut && len(p) == 1 && p[0] == "config":
		cfg := s.state["config"].(map[string]any)
		mergeInto(cfg, body)
		var out []any
		for k, v := range body {
			out = append(out, map[string]any{"success": map[string]any{"/config/" + k: v}})
		}
		ok(out...)
	case r.Method == http.MethodDelete && len(p) == 3 && p[0] == "config" && p[1] == "whitelist":
		if _, there := wl[p[2]]; !there {
			v1err(3, "/config/whitelist/"+p[2], "resource not available")
			return
		}
		delete(wl, p[2])
		ok(map[string]any{"success": "/config/whitelist/" + p[2] + " deleted"})
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && (len(p) == 2 || len(p) == 3) && storeSections[p[0]]:
		sec := s.state[p[0]].(map[string]any)
		item, there := sec[p[1]].(map[string]any)
		if !there {
			v1err(3, "/"+strings.Join(p, "/"), "resource, /"+p[0]+"/"+p[1]+", not available")
			return
		}
		if r.Method == http.MethodDelete {
			delete(sec, p[1])
			ok(map[string]any{"success": "/" + p[0] + "/" + p[1] + " deleted"})
			return
		}
		target := item
		if len(p) == 3 {
			sub, _ := item[p[2]].(map[string]any)
			if sub == nil {
				sub = map[string]any{}
				item[p[2]] = sub
			}
			target = sub
		}
		if _, bad := body["owner"]; bad && len(p) == 2 {
			v1err(6, "/"+strings.Join(p, "/")+"/owner", "parameter, owner, not available")
			return
		}
		var out []any
		for k, v := range body {
			target[k] = v
			out = append(out, map[string]any{"success": map[string]any{"/" + strings.Join(p, "/") + "/" + k: v}})
		}
		ok(out...)
	default:
		v1err(4, r.URL.Path, "method not available")
	}
}
