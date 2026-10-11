package hue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// The bridge's HTTP interface (Hue API v2 "Getting started"):
//
//	POST /api                         create an application key (link button)
//	GET  /clip/v2/resource/<type>     list resources of a type
//	PUT  /clip/v2/resource/<type>/<id>
//	GET  /eventstream/clip/v2         server-sent events
//
// Every CLIP v2 request carries the key in the hue-application-key header;
// answers are {"errors": [{"description": …}], "data": [...]}.

// KeyHeader is the header that carries the application key.
const KeyHeader = "hue-application-key"

// ResourcePath is the path prefix of CLIP v2 resources.
const ResourcePath = "/clip/v2/resource/"

// EventStreamPath is the path of the bridge's event stream.
const EventStreamPath = "/eventstream/clip/v2"

// Redacted replaces the application key in error texts.
const Redacted = "(hidden)"

// ErrLinkButton is returned by Pair when the bridge's link button was not
// pressed (bridge error type 101).
var ErrLinkButton = errors.New("the bridge's link button was not pressed")

// ErrUnauthorized is returned when the bridge refuses the application key.
var ErrUnauthorized = errors.New("the bridge refused the application key")

// Config configures a Client.
type Config struct {
	// Address is the bridge's IP address, host name, host:port, [v6]:port
	// or https URL.
	Address string
	// AppKey is the application key; empty for pairing and public reads.
	AppKey string
	// TLS is the certificate check. Bridges present a certificate whose
	// common name is the bridge ID, so it is checked by TLS.Verify rather
	// than by host name.
	TLS TLSCheck
	// Timeout bounds one request (default 10s); the event stream has none.
	Timeout time.Duration
	// DialContext replaces the TCP dialer (default: 5s connect timeout,
	// 30s TCP keep-alive, which also watches the quiet event stream).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// PlainHTTP is the client for pairing a bridge that answers only
	// plain HTTP (API v1); default an http.Client with Timeout.
	PlainHTTP *http.Client
}

// Client talks to one bridge address.
type Client struct {
	base    string // https://host[:port]
	key     string
	hc      *http.Client // requests with a timeout
	stream  *http.Client // the event stream (no timeout)
	plain   *http.Client
	timeout time.Duration
	// V1, when set, serves the CLIP v2 resource requests of Do from API v1
	// (a bridge without CLIP v2); see V1Client.
	V1 *V1Client
}

// NewClient returns a client for the bridge at cfg.Address.
func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	dial := cfg.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	tr := &http.Transport{
		TLSClientConfig:     cfg.TLS.Config(),
		DialContext:         dial,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	plain := cfg.PlainHTTP
	if plain == nil {
		plain = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{base: BaseURL(cfg.Address), key: cfg.AppKey, hc: &http.Client{Transport: tr, Timeout: cfg.Timeout},
		stream: &http.Client{Transport: tr}, plain: plain, timeout: cfg.Timeout}
}

// BaseURL is the bridge's https base URL.
func (c *Client) BaseURL() string { return c.base }

// HTTPClient is the client for requests to the bridge, with the
// certificate check and the timeout.
func (c *Client) HTTPClient() *http.Client { return c.hc }

// CloseIdleConnections closes the client's idle connections.
func (c *Client) CloseIdleConnections() {
	if tr, ok := c.hc.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// BaseURL turns an address (IP, host, host:port, [v6]:port or a URL) into
// the bridge's https base URL.
func BaseURL(address string) string {
	a := strings.TrimSpace(address)
	if strings.HasPrefix(a, "https://") || strings.HasPrefix(a, "http://") {
		return strings.TrimRight(a, "/")
	}
	if _, _, err := net.SplitHostPort(a); err != nil {
		if ip := net.ParseIP(strings.Trim(a, "[]")); ip != nil && ip.To4() == nil {
			a = "[" + strings.Trim(a, "[]") + "]"
		}
	}
	return "https://" + a
}

// APIError is an error answer of the bridge.
type APIError struct {
	// Status is the HTTP status.
	Status int
	// Desc are the error descriptions the answer carried.
	Desc []string
}

// Error names the HTTP status and the descriptions.
func (e *APIError) Error() string {
	if len(e.Desc) == 0 {
		return fmt.Sprintf("bridge answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("bridge answered HTTP %d: %s", e.Status, strings.Join(e.Desc, "; "))
}

type envelope struct {
	Errors []struct {
		Description string `json:"description"`
	} `json:"errors"`
	Data []json.RawMessage `json:"data"`
}

// Do sends a CLIP v2 request (path from the base URL, body marshalled as
// JSON when not nil) and returns the data items. A refused key is
// ErrUnauthorized; an error answer, or a change answered with errors
// (a partly applied body), is an *APIError (with the data for a change).
func (c *Client) Do(ctx context.Context, method, path string, body any) ([]json.RawMessage, error) {
	if c.V1 != nil {
		return c.V1.Do(ctx, method, path, body)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set(KeyHeader, c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, Scrub(err, c.key)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var env envelope
	_ = json.Unmarshal(b, &env)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		e := &APIError{Status: resp.StatusCode}
		for _, x := range env.Errors {
			e.Desc = append(e.Desc, x.Description)
		}
		return nil, e
	}
	if len(env.Errors) > 0 && method != http.MethodGet {
		e := &APIError{Status: resp.StatusCode}
		for _, x := range env.Errors {
			e.Desc = append(e.Desc, x.Description)
		}
		return env.Data, e
	}
	return env.Data, nil
}

// List reads every resource of a type.
func (c *Client) List(ctx context.Context, rtype string) ([]Resource, error) {
	items, err := c.Do(ctx, http.MethodGet, ResourcePath+rtype, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(items))
	for _, raw := range items {
		var r Resource
		if json.Unmarshal(raw, &r) == nil && r.ID() != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// Put changes a resource (PUT /clip/v2/resource/<type>/<id>).
func (c *Client) Put(ctx context.Context, rtype, id string, body any) error {
	_, err := c.Do(ctx, http.MethodPut, ResourcePath+rtype+"/"+id, body)
	return err
}

// Events opens the bridge's event stream; read it with ReadEvents and
// close it when done. An answer other than 200 is an *APIError; API v1
// has no event stream.
func (c *Client) Events(ctx context.Context) (io.ReadCloser, error) {
	if c.V1 != nil {
		return nil, errors.New("API v1 has no event stream")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+EventStreamPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(KeyHeader, c.key)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, Scrub(err, c.key)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, &APIError{Status: resp.StatusCode}
	}
	return resp.Body, nil
}

// Scrub keeps an application key out of an error's text.
func Scrub(err error, key string) error {
	if err == nil || key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, Redacted))
}

// PairResult is what the bridge issues when its link button was pressed.
type PairResult struct {
	// AppKey is the application key (the v1 "username").
	AppKey string
	// ClientKey is the entertainment client key; empty from API v1.
	ClientKey string
}

// Pair asks the bridge for an application key (POST /api with
// generateclientkey). deviceType names the application to the bridge as
// "<application>#<instance>". ErrLinkButton means the button was not
// pressed. A bridge that does not answer HTTPS (the round bridge) is asked
// over plain HTTP, without a client key.
func (c *Client) Pair(ctx context.Context, deviceType string) (PairResult, error) {
	body, _ := json.Marshal(map[string]any{"devicetype": deviceType, "generateclientkey": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api", bytes.NewReader(body))
	if err != nil {
		return PairResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		// A round (v1) bridge has no HTTPS: pair over HTTP, without the
		// client key it does not know.
		body, _ = json.Marshal(map[string]any{"devicetype": deviceType})
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, V1Base(strings.TrimPrefix(c.base, "https://"))+"/api", bytes.NewReader(body))
		if rerr != nil {
			return PairResult{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		var herr error
		if resp, herr = c.plain.Do(req); herr != nil {
			return PairResult{}, err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PairResult{}, err
	}
	var items []struct {
		Success *struct {
			Username  string `json:"username"`
			ClientKey string `json:"clientkey"`
		} `json:"success"`
		Error *struct {
			Type        int    `json:"type"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &items); err != nil || len(items) == 0 {
		return PairResult{}, fmt.Errorf("the bridge's answer to pairing is not understood (HTTP %d)", resp.StatusCode)
	}
	if e := items[0].Error; e != nil {
		if e.Type == 101 {
			return PairResult{}, ErrLinkButton
		}
		return PairResult{}, fmt.Errorf("the bridge refused pairing: error %d %s", e.Type, e.Description)
	}
	if s := items[0].Success; s != nil && s.Username != "" {
		return PairResult{AppKey: s.Username, ClientKey: s.ClientKey}, nil
	}
	return PairResult{}, errors.New("the bridge's answer to pairing has no key")
}
