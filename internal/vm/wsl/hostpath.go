package wsl

import (
	"fmt"

	"github.com/tricknotes/merry-desktop/internal/engineapi"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

var _ vm.HostPathMapper = (*Backend)(nil)

// VMPath maps a Windows path to the VM: drives are under /mnt/<letter>, and
// paths in integrated distros (\\wsl.localhost\<distro>\...) are where the
// distro's root is bind-mounted, as for bind mounts from the Windows CLI.
func (b *Backend) VMPath(hostPath string) (string, error) {
	if p, ok := engineapi.WindowsPaths(b.distroRoot())(b.hostPath(hostPath)); ok {
		return p, nil
	}
	return "", fmt.Errorf("この場所は VM から使えません: %s", hostPath)
}
