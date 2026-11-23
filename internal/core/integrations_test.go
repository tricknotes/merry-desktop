package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

type integrating struct {
	*provisioning
	names []string
	calls int
}

func (i *integrating) Integrations(context.Context) ([]vm.Integration, error) {
	return []vm.Integration{{Name: "Ubuntu", Default: true, Enabled: true}}, nil
}
func (i *integrating) SetIntegrations(_ context.Context, names []string) error {
	i.names, i.calls = names, i.calls+1
	return nil
}

func TestApplySavedIntegrations(t *testing.T) {
	b := &integrating{provisioning: newProvisioning()}
	c := NewController(b, nil)
	if err := c.ApplySavedIntegrations(context.Background(), nil); err != nil || b.calls != 0 {
		t.Fatalf("nil saved choice must not call the backend (calls=%d, err=%v)", b.calls, err)
	}
	none := []string{}
	if err := c.ApplySavedIntegrations(context.Background(), &none); err != nil || b.names == nil || len(b.names) != 0 {
		t.Fatalf("empty choice = %#v, %v", b.names, err)
	}
	got, _ := c.Integrations(context.Background())
	if !reflect.DeepEqual(got, []vm.Integration{{Name: "Ubuntu", Default: true, Enabled: true}}) {
		t.Errorf("Integrations = %+v", got)
	}
	if err := NewController(newStub(), nil).SetIntegrations(context.Background(), nil); err == nil {
		t.Error("a backend without integrations must refuse")
	}
}
