package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// NormalizeRegistry turns user input such as "https://registry.example.com/"
// into the host[:port] form that `docker login` and image names use.
func NormalizeRegistry(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimRight(s, "/")
	if s == "" || strings.ContainsAny(s, " /\\\t@") {
		return "", fmt.Errorf("レジストリは「ホスト名」か「ホスト名:ポート」で入力してください: %q", s)
	}
	host := s
	if h, port, err := net.SplitHostPort(s); err == nil {
		if port == "" {
			return "", fmt.Errorf("ポート番号がありません: %q", s)
		}
		host = h
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", fmt.Errorf("ホスト名が正しくありません: %q", s)
	}
	return strings.ToLower(s), nil
}

// Login signs the host docker CLI in to a registry (F-12). The password is
// handed to the backend only; it is never stored or logged here.
func (c *Controller) Login(ctx context.Context, server, username, password string) error {
	l, ok := c.backend.(vm.RegistryLogin)
	if !ok {
		return vm.ErrNotSupported
	}
	server, err := NormalizeRegistry(server)
	if err != nil {
		return err
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return errors.New("ユーザー名とパスワードを入力してください")
	}
	if err := l.Login(ctx, server, strings.TrimSpace(username), password); err != nil {
		c.log.Warn("registry login failed", "server", server) // without the error: it may echo input
		return err
	}
	c.log.Info("registry login", "server", server)
	return nil
}
