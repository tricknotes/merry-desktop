package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// SetupState is the state of one step of the setup wizard.
type SetupState string

const (
	SetupRunning SetupState = "running"
	SetupDone    SetupState = "done"
	SetupSkipped SetupState = "skipped"
	SetupFailed  SetupState = "failed"
)

// SetupEvent reports progress of the setup wizard (F-10) to the UI.
type SetupEvent struct {
	Step    vm.SetupStep `json:"step"`
	State   SetupState   `json:"state"`
	Message string       `json:"message"`
}

// helloWorld is the final check of the wizard. The image is pulled from the
// registry configured in the engine, which also proves the network settings.
var helloWorld = []string{"docker", "run", "--rm", "hello-world"}

// NeedsSetup reports whether the VM still has to be created.
func (c *Controller) NeedsSetup(ctx context.Context) (bool, error) {
	p, ok := c.backend.(vm.Provisioner)
	if !ok {
		return false, nil
	}
	provisioned, err := p.Provisioned(ctx)
	return !provisioned, err
}

// Setup runs the wizard: check the host, read the network settings, create
// the VM, start it and run hello-world. Each step is reported through emit.
// Running it again skips the steps that are already done.
func (c *Controller) Setup(ctx context.Context, emit func(SetupEvent)) error {
	if emit == nil {
		emit = func(SetupEvent) {}
	}
	p, ok := c.backend.(vm.Provisioner)
	if !ok {
		return vm.ErrNotSupported
	}
	if !c.begin() {
		return errors.New("別の操作の実行中です。終わってからもう一度試してください")
	}
	defer c.end()

	fail := func(step vm.SetupStep, err error) error {
		c.log.Error("setup failed", "step", step, "err", err)
		emit(SetupEvent{Step: step, State: SetupFailed, Message: setupMessage(err)})
		return err
	}

	emit(SetupEvent{Step: vm.StepCheckHost, State: SetupRunning, Message: "WSL2 が使えるか確認しています"})
	if err := p.CheckHost(ctx); err != nil {
		return fail(vm.StepCheckHost, err)
	}
	emit(SetupEvent{Step: vm.StepCheckHost, State: SetupDone, Message: "WSL2 を使えます"})

	emit(SetupEvent{Step: vm.StepNetwork, State: SetupRunning, Message: "プロキシと証明書の設定を読み取っています"})
	network, err := p.DetectNetwork(ctx)
	if err != nil {
		return fail(vm.StepNetwork, err)
	}
	emit(SetupEvent{Step: vm.StepNetwork, State: SetupDone, Message: describeNetwork(network)})

	provisioned, err := p.Provisioned(ctx)
	if err != nil {
		return fail(vm.StepCreate, err)
	}
	if provisioned {
		emit(SetupEvent{Step: vm.StepCreate, State: SetupSkipped, Message: "VM はすでにあります"})
	} else {
		emit(SetupEvent{Step: vm.StepCreate, State: SetupRunning, Message: "VM を作成しています"})
		progress := func(line string) { emit(SetupEvent{Step: vm.StepCreate, State: SetupRunning, Message: line}) }
		if err := p.Provision(ctx, network, progress); err != nil {
			return fail(vm.StepCreate, err)
		}
		emit(SetupEvent{Step: vm.StepCreate, State: SetupDone, Message: "VM を作成しました"})
	}

	emit(SetupEvent{Step: vm.StepStart, State: SetupRunning, Message: "VM を起動しています"})
	c.start(ctx)
	if s := c.Snapshot(); s.Status != vm.StatusRunning {
		return fail(vm.StepStart, errors.New(s.Message))
	}
	emit(SetupEvent{Step: vm.StepStart, State: SetupDone, Message: "VM が起動しました"})

	emit(SetupEvent{Step: vm.StepVerify, State: SetupRunning, Message: "hello-world を実行しています"})
	res, err := c.backend.Exec(ctx, helloWorld)
	if err != nil {
		return fail(vm.StepVerify, err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "Hello from Docker!") {
		return fail(vm.StepVerify, fmt.Errorf("hello-world が失敗しました(終了コード %d): %s", res.ExitCode, lastLine(res.Stderr+res.Stdout)))
	}
	emit(SetupEvent{Step: vm.StepVerify, State: SetupDone, Message: "hello-world が動きました"})

	if h, ok := c.backend.(vm.HostIntegrator); ok && !c.SkipHostIntegration {
		emit(SetupEvent{Step: vm.StepHostCLI, State: SetupRunning, Message: "ターミナルから docker を使えるようにしています"})
		progress := func(line string) { emit(SetupEvent{Step: vm.StepHostCLI, State: SetupRunning, Message: line}) }
		if err := h.IntegrateHost(ctx, network, progress); err != nil {
			return fail(vm.StepHostCLI, err)
		}
		emit(SetupEvent{Step: vm.StepHostCLI, State: SetupDone, Message: "新しいターミナルで docker と docker compose を使えます"})
	}
	return nil
}

func describeNetwork(n vm.Network) string {
	parts := []string{"プロキシ: なし"}
	if n.ProxyURL != "" {
		parts[0] = "プロキシ: あり"
	}
	if certs := bytes.Count(n.CACertsPEM, []byte("BEGIN CERTIFICATE")); certs > 0 {
		parts = append(parts, fmt.Sprintf("取り込む証明書: %d 件", certs))
	}
	if n.Note != "" {
		parts = append(parts, n.Note)
	}
	return strings.Join(parts, "、")
}

func setupMessage(err error) string {
	if errors.Is(err, vm.ErrHostNotReady) {
		// The wrapping message is written for the user.
		_, detail, _ := strings.Cut(err.Error(), ": ")
		return detail
	}
	return err.Error()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
