// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package entertainment streams colours to a Philips Hue bridge's
// entertainment area: HueStream frames (version 2) over DTLS 1.2 with a
// pre-shared key, in pure Go (pion/dtls, no cgo).
//
// The steps: create an entertainment_configuration (or pick one) through
// CLIP v2, start it (action start), open the DTLS session to UDP port
// 2100 of the bridge with the application ID (GET /auth/v1) as PSK
// identity and the client key from pairing as the key, then send frames
// of channel colours, many times a second. Open does the CLIP v2 steps
// with a hue.Client; Dial is the DTLS part alone.
//
// It is a module of its own, so that github.com/cB-Sigma/hue keeps no
// dependency beyond the standard library.
package entertainment

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cB-Sigma/hue"
	"github.com/pion/dtls/v3"
)

// Port is the bridge's UDP port for entertainment streaming.
const Port = 2100

// CipherSuite is the one cipher suite Hue bridges accept for streaming.
const CipherSuite = dtls.TLS_PSK_WITH_AES_128_GCM_SHA256

// Config configures Dial.
type Config struct {
	// Addr is the bridge's host:port (host alone means Port).
	Addr string
	// Identity is the PSK identity: the application ID of the key that
	// started the configuration (hue.Client.ApplicationID).
	Identity string
	// PSK is the key: the client key from pairing, decoded from hex
	// (ParseClientKey).
	PSK []byte
	// ConfigID is the entertainment configuration's ID.
	ConfigID string
	// Space is the colour space of the frames (RGB when zero).
	Space ColourSpace
	// Repeat, when positive, resends the last frame at this interval
	// until the next Send or Close, so a bridge that ends quiet streams
	// (after about ten seconds) and a lossy network still see colours.
	Repeat time.Duration
}

// ParseClientKey decodes a client key as pairing returns it (32
// hexadecimal digits).
func ParseClientKey(s string) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != 16 {
		return nil, errors.New("the client key is not 32 hexadecimal digits")
	}
	return k, nil
}

// Stream is an open DTLS session to an entertainment area.
type Stream struct {
	conn  net.Conn
	cfg   Config
	mu    sync.Mutex
	seq   uint8
	last  []byte
	stop  chan struct{}
	done  chan struct{}
	close sync.Once
	err   error
}

// Dial opens the DTLS session (handshake bounded by ctx).
func Dial(ctx context.Context, cfg Config) (*Stream, error) {
	if cfg.Identity == "" || len(cfg.PSK) == 0 {
		return nil, errors.New("streaming needs the application ID and the client key")
	}
	if len(cfg.ConfigID) != 36 {
		return nil, fmt.Errorf("the configuration ID %q is not a UUID", cfg.ConfigID)
	}
	addr := cfg.Addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), fmt.Sprint(Port))
	}
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	psk := append([]byte(nil), cfg.PSK...)
	dc, err := dtls.Dial("udp", raddr, &dtls.Config{
		PSK:                  func([]byte) ([]byte, error) { return psk, nil },
		PSKIdentityHint:      []byte(cfg.Identity),
		CipherSuites:         []dtls.CipherSuiteID{CipherSuite},
		ExtendedMasterSecret: dtls.RequestExtendedMasterSecret,
	})
	if err != nil {
		return nil, err
	}
	if err := dc.HandshakeContext(ctx); err != nil {
		_ = dc.Close()
		return nil, fmt.Errorf("DTLS handshake with %s: %w", addr, err)
	}
	s := &Stream{conn: dc, cfg: cfg, stop: make(chan struct{}), done: make(chan struct{})}
	if cfg.Repeat > 0 {
		go s.repeat()
	} else {
		close(s.done)
	}
	return s, nil
}

func (s *Stream) repeat() {
	defer close(s.done)
	t := time.NewTicker(s.cfg.Repeat)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			if s.last != nil {
				s.last[11] = s.seq
				s.seq++
				_, _ = s.conn.Write(s.last)
			}
			s.mu.Unlock()
		}
	}
}

// Send sends one frame with these channel colours (in the stream's
// colour space).
func (s *Stream) Send(channels ...Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	b, err := Frame{ConfigID: s.cfg.ConfigID, Space: s.cfg.Space, Seq: s.seq, Channels: channels}.MarshalBinary()
	if err != nil {
		return err
	}
	s.seq++
	s.last = b
	if _, err := s.conn.Write(b); err != nil {
		s.err = err
		return err
	}
	return nil
}

// LocalAddr is the stream's local UDP address.
func (s *Stream) LocalAddr() net.Addr { return s.conn.LocalAddr() }

// Close ends the DTLS session (the configuration stays started; see
// Session.Close).
func (s *Stream) Close() error {
	var err error
	s.close.Do(func() {
		close(s.stop)
		<-s.done
		s.mu.Lock()
		if s.err == nil {
			s.err = net.ErrClosed
		}
		s.mu.Unlock()
		err = s.conn.Close()
	})
	return err
}

// Options configure Open.
type Options struct {
	// Addr overrides the streaming address (default the bridge's host
	// and Port).
	Addr string
	// Space is the colour space (RGB when zero).
	Space ColourSpace
	// Repeat is Config.Repeat.
	Repeat time.Duration
}

// Session is a started entertainment configuration and its stream.
type Session struct {
	*Stream
	c        *hue.Client
	configID string
}

// Open starts an entertainment configuration through CLIP v2 and opens
// its stream: it reads the application ID of c's key, starts the
// configuration and dials the bridge. clientKey is the client key from
// pairing (hex). When the dial fails, the configuration is stopped again.
func Open(ctx context.Context, c *hue.Client, configID, clientKey string, o Options) (*Session, error) {
	psk, err := ParseClientKey(clientKey)
	if err != nil {
		return nil, err
	}
	appID, err := c.ApplicationID(ctx)
	if err != nil {
		return nil, err
	}
	addr := o.Addr
	if addr == "" {
		u, err := url.Parse(c.BaseURL())
		if err != nil {
			return nil, err
		}
		addr = net.JoinHostPort(u.Hostname(), fmt.Sprint(Port))
	}
	if err := c.StartEntertainment(ctx, configID); err != nil {
		return nil, err
	}
	st, err := Dial(ctx, Config{Addr: addr, Identity: appID, PSK: psk, ConfigID: configID, Space: o.Space, Repeat: o.Repeat})
	if err != nil {
		_ = c.StopEntertainment(context.WithoutCancel(ctx), configID)
		return nil, err
	}
	return &Session{Stream: st, c: c, configID: configID}, nil
}

// Close ends the stream and stops the configuration.
func (s *Session) Close(ctx context.Context) error {
	err := s.Stream.Close()
	if serr := s.c.StopEntertainment(ctx, s.configID); serr != nil && err == nil {
		err = serr
	}
	return err
}
