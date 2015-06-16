// Copyright 2014 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License. See the AUTHORS file
// for names of contributors.
//
// Author: jqmp (jaqueramaphan@gmail.com)

package security

// TODO(jqmp): The use of TLS here is just a proof of concept; its security
// properties haven't been analyzed or audited.

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"

	"github.com/cockroachdb/cockroach/util"
	"github.com/cockroachdb/cockroach/util/log"
)

const (
	// EmbeddedCertsDir is the certs directory inside embedded assets.
	EmbeddedCertsDir = "test_certs"
)

// readFileFn is used to mock out file system access during tests.
var readFileFn = ioutil.ReadFile

// SetReadFileFn allows to switch out ioutil.ReadFile by a mock
// for testing purposes.
func SetReadFileFn(f func(string) ([]byte, error)) {
	readFileFn = f
}

// ResetReadFileFn is the counterpart to SetReadFileFn, restoring the
// original behaviour for loading certificate related data from disk.
func ResetReadFileFn() {
	readFileFn = ioutil.ReadFile
}

// LoadTLSConfigFromDir creates a TLSConfig by loading our keys and certs from the
// specified directory. The directory must contain the following files:
// - ca.crt   -- the certificate of the cluster CA
// - node.server.crt -- the server certificate of this node; should be signed by the CA
// - node.server.key -- the certificate key
// If the path is prefixed with "embedded=", load the embedded certs.
func LoadTLSConfigFromDir(certDir string) (*tls.Config, error) {
	certPEM, err := readFileFn(path.Join(certDir, "node.server.crt"))
	if err != nil {
		return nil, err
	}
	keyPEM, err := readFileFn(path.Join(certDir, "node.server.key"))
	if err != nil {
		return nil, err
	}
	caPEM, err := readFileFn(path.Join(certDir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	return LoadTLSConfig(certPEM, keyPEM, caPEM)
}

// LoadTLSConfig creates a TLSConfig from the supplied byte strings containing
// - the certificate of this node (should be signed by the CA),
// - the private key of this node.
// - the certificate of the cluster CA,
func LoadTLSConfig(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
		err = util.Error("failed to parse PEM data to pool")
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		// Verify client certs if passed.
		ClientAuth: tls.VerifyClientCertIfGiven,
		RootCAs:    certPool,
		ClientCAs:  certPool,

		// Use the default cipher suite from golang (RC4 is going away in 1.5).
		// Prefer the server-specified suite.
		PreferServerCipherSuites: true,

		// TLS 1.1 and 1.2 support is crappy out there. Let's use 1.0.
		MinVersion: tls.VersionTLS10,

		// Should we disable session resumption? This may break forward secrecy.
		// SessionTicketsDisabled: true,
	}, nil
}

// LoadInsecureTLSConfig creates a TLSConfig that disables TLS.
func LoadInsecureTLSConfig() *tls.Config {
	return nil
}

// LoadClientTLSConfigFromDir creates a client TLSConfig by loading the root CA certs from the
// specified directory. The directory must contain the following files:
// - ca.crt   -- the certificate of the cluster CA
// - node.client.crt -- the client certificate of this node; should be signed by the CA
// - node.client.key -- the certificate key
// If the path is prefixed with "embedded=", load the embedded certs.
func LoadClientTLSConfigFromDir(certDir string) (*tls.Config, error) {
	certPEM, err := readFileFn(path.Join(certDir, "node.client.crt"))
	if err != nil {
		return nil, err
	}
	keyPEM, err := readFileFn(path.Join(certDir, "node.client.key"))
	if err != nil {
		return nil, err
	}
	caPEM, err := readFileFn(path.Join(certDir, "ca.crt"))
	if err != nil {
		return nil, err
	}

	return LoadClientTLSConfig(certPEM, keyPEM, caPEM)
}

// LoadClientTLSConfig creates a client TLSConfig from the supplied byte strings containing:
// - the certificate of this client (should be signed by the CA),
// - the private key of this client.
// - the certificate of the cluster CA,
func LoadClientTLSConfig(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
		err := util.Error("failed to parse PEM data to pool")
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// LoadInsecureClientTLSConfig creates a TLSConfig that disables TLS.
func LoadInsecureClientTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
	}
}

// LogRequestCertificates examines a http request and logs a summary of the TLS config.
func LogRequestCertificates(r *http.Request) {
	if r.TLS == nil {
		if log.V(3) {
			log.Infof("%s %s: no TLS", r.Method, r.URL)
		}
		return
	}

	peerCerts := []string{}
	verifiedChain := []string{}
	for _, cert := range r.TLS.PeerCertificates {
		peerCerts = append(peerCerts, fmt.Sprintf("%s (%s, %s)", cert.Subject.CommonName, cert.DNSNames, cert.IPAddresses))
	}
	for _, chain := range r.TLS.VerifiedChains {
		subjects := []string{}
		for _, cert := range chain {
			subjects = append(subjects, cert.Subject.CommonName)
		}
		verifiedChain = append(verifiedChain, strings.Join(subjects, ","))
	}
	if log.V(3) {
		log.Infof("%s %s: peer certs: %v, chain: %v", r.Method, r.URL, peerCerts, verifiedChain)
	}
}
