// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// TLSCheck is the certificate check for one bridge. A bridge presents a
// certificate issued by the Hue root CA whose common name is its bridge
// ID; it has no name a public CA would vouch for, so the check is: the
// common name must be the bridge ID, the chain must verify against Roots
// when given, and (through Pin) the certificate should be the one seen
// before. What it finds is handed to the callbacks, which decide.
//
// The zero TLSCheck refuses any certificate whose common name or chain
// cannot be checked and pins nothing.
type TLSCheck struct {
	// BridgeID returns the expected bridge ID ("" when not known yet).
	BridgeID func() string
	// Roots, when not nil, are the CAs the chain must verify against
	// (the Hue bridge root CA, which this package does not ship).
	Roots *x509.CertPool
	// Pin, when not nil, is told the bridge ID and the SHA-256 of the
	// leaf (hex) when the bridge ID is known; an error refuses the
	// connection. Remembering certificates per bridge is the caller's.
	Pin func(bridgeID, fingerprint string) error
	// Unverified, when not nil, decides a certificate with problems
	// (subject is the bridge ID, or the server name when the ID is not
	// known): nil accepts it, an error refuses it. When nil, such a
	// certificate is refused.
	Unverified func(subject, fingerprint string, problems []string) error
}

// Config is a tls.Config that runs the check (TLS 1.2 or later).
func (c TLSCheck) Config() *tls.Config {
	// The certificate is checked by Verify, not by name.
	return &tls.Config{InsecureSkipVerify: true, VerifyConnection: c.Verify, MinVersion: tls.VersionTLS12} //nolint:gosec // verified in VerifyConnection
}

// Fingerprint is the hex SHA-256 of a certificate's DER.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Verify checks a connection's certificate (a tls.Config
// VerifyConnection).
func (c TLSCheck) Verify(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("the bridge presented no certificate")
	}
	leaf := cs.PeerCertificates[0]
	id := ""
	if c.BridgeID != nil {
		id = c.BridgeID()
	}
	subject := id
	if subject == "" {
		subject = cs.ServerName
	}
	var problems []string
	if id == "" || !strings.EqualFold(leaf.Subject.CommonName, id) {
		problems = append(problems, fmt.Sprintf("its common name %q is not the bridge ID %q", leaf.Subject.CommonName, id))
	}
	if c.Roots != nil {
		inter := x509.NewCertPool()
		for _, x := range cs.PeerCertificates[1:] {
			inter.AddCert(x)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: c.Roots, Intermediates: inter, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
			problems = append(problems, "its chain does not verify against the trusted roots: "+err.Error())
		}
	}
	fp := Fingerprint(leaf)
	if id != "" && c.Pin != nil {
		if err := c.Pin(id, fp); err != nil {
			return err
		}
	}
	if len(problems) > 0 {
		if c.Unverified != nil {
			return c.Unverified(subject, fp, problems)
		}
		return fmt.Errorf("bridge %s certificate refused: %s", subject, strings.Join(problems, "; "))
	}
	return nil
}
