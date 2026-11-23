package core

import (
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestClientProxyFor(t *testing.T) {
	got, err := ClientProxyFor(vm.Network{ProxyURL: "http://proxy:3128", NoProxy: "localhost,.corp"})
	if err != nil || got != (vm.ClientProxy{HTTPProxy: "http://proxy:3128", HTTPSProxy: "http://proxy:3128", NoProxy: "localhost,.corp"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, n := range []vm.Network{{}, {ProxyURL: "http://alice:pw@proxy:3128"}, {ProxyURL: "://bad"}} {
		if _, err := ClientProxyFor(n); err == nil {
			t.Errorf("%+v: want an error", n)
		}
	}
}
