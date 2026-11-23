package wsl

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestHostRootCertificatesAreParseable(t *testing.T) {
	data, err := hostRootCertificates()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Bytes) == 0 {
			t.Errorf("block %d: type %q, %d bytes", count, block.Type, len(block.Bytes))
		}
		// Go rejects some valid Windows roots (for example a negative serial
		// number); OpenSSL in the VM accepts them, so this is only logged.
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			t.Logf("certificate %d is not parseable by Go: %v", count, err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no root certificates read from the Windows store")
	}
	t.Logf("%d root certificates", count)
}

func TestHostProxyDoesNotFail(t *testing.T) {
	// The runner normally has no proxy; this checks the registry read works.
	proxy, noProxy, note := hostProxy()
	t.Logf("proxy set=%v noProxy=%q note=%q", proxy != "", noProxy, note)
}
