package wsl

import (
	"context"
	"os"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// DetectNetwork reads the host proxy and root certificates (F-11):
//  1. HTTPS_PROXY / HTTP_PROXY / NO_PROXY environment variables, if set;
//  2. otherwise the WinINET proxy of the user (Windows only).
//
// Root certificates come from the Windows "Trusted Root Certification
// Authorities" store, which is where a corporate CA is normally installed.
// Public roots are included as well; duplicates in the distro are harmless.
func (b *Backend) DetectNetwork(context.Context) (vm.Network, error) {
	var n vm.Network
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(k); v != "" {
			n.ProxyURL = v
			break
		}
	}
	if n.ProxyURL != "" {
		for _, k := range []string{"NO_PROXY", "no_proxy"} {
			if v := os.Getenv(k); v != "" {
				n.NoProxy = v
				break
			}
		}
	} else {
		n.ProxyURL, n.NoProxy, n.Note = hostProxy()
	}
	certs, err := hostRootCertificates()
	if err != nil {
		return n, err
	}
	n.CACertsPEM = certs
	return n, nil
}
