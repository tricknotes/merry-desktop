package core

import (
	"context"
	"errors"
	"net/url"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// ClientProxyFor turns the detected network settings into the values for the
// docker CLI config. A proxy with credentials is refused: the config file is
// plain text.
func ClientProxyFor(n vm.Network) (vm.ClientProxy, error) {
	if n.ProxyURL == "" {
		return vm.ClientProxy{}, errors.New("プロキシが設定されていません")
	}
	if u, err := url.Parse(n.ProxyURL); err != nil {
		return vm.ClientProxy{}, errors.New("プロキシの URL が正しくありません")
	} else if u.User != nil {
		return vm.ClientProxy{}, errors.New("認証情報つきのプロキシは、平文の設定ファイルに書けないため設定しません")
	}
	return vm.ClientProxy{HTTPProxy: n.ProxyURL, HTTPSProxy: n.ProxyURL, NoProxy: n.NoProxy}, nil
}

// EnableBuildProxy writes the detected proxy into the docker CLI configs (C-4)
// and returns what was written, to be kept for DisableBuildProxy.
func (c *Controller) EnableBuildProxy(ctx context.Context) (vm.ClientProxy, []string, error) {
	cfg, ok := c.backend.(vm.ClientProxyConfigurer)
	p, ok2 := c.backend.(vm.Provisioner)
	if !ok || !ok2 {
		return vm.ClientProxy{}, nil, vm.ErrNotSupported
	}
	n, err := p.DetectNetwork(ctx)
	if err != nil {
		return vm.ClientProxy{}, nil, err
	}
	proxy, err := ClientProxyFor(n)
	if err != nil {
		return vm.ClientProxy{}, nil, err
	}
	lines, err := cfg.ConfigureClientProxy(ctx, proxy, true)
	return proxy, lines, err
}

// DisableBuildProxy removes the values written by EnableBuildProxy.
func (c *Controller) DisableBuildProxy(ctx context.Context, written vm.ClientProxy) ([]string, error) {
	cfg, ok := c.backend.(vm.ClientProxyConfigurer)
	if !ok {
		return nil, vm.ErrNotSupported
	}
	return cfg.ConfigureClientProxy(ctx, written, false)
}
