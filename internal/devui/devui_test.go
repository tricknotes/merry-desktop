package devui

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type fakeService struct{ last string }

type pair struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func (f *fakeService) Echo(p pair, n int) (pair, error) { p.A += n; return p, nil }
func (f *fakeService) Fail() error                      { return errors.New("無効な値です") }
func (f *fakeService) Nothing()                         { f.last = "called" }

func newTest(t *testing.T) (*Server, *httptest.Server, *fakeService) {
	t.Helper()
	svc := &fakeService{}
	s, err := New(svc, fstest.MapFS{"index.html": {Data: []byte("<h1>settings</h1>")}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts, svc
}

func post(t *testing.T, url, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Merry-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestCall(t *testing.T) {
	s, ts, svc := newTest(t)
	if code, body := post(t, ts.URL+"/api/call/Echo", s.Token, `[{"a":1,"b":"x"},2]`); code != 200 || body != `{"a":3,"b":"x"}` {
		t.Errorf("Echo = %d %s", code, body)
	}
	if code, body := post(t, ts.URL+"/api/call/Fail", s.Token, `[]`); code != 422 || body != "無効な値です" {
		t.Errorf("Fail = %d %s", code, body)
	}
	if code, _ := post(t, ts.URL+"/api/call/Nothing", s.Token, `[]`); code != 200 || svc.last != "called" {
		t.Errorf("Nothing = %d, called=%q", code, svc.last)
	}
	for _, bad := range []struct{ path, body string }{
		{"/api/call/Missing", "[]"}, {"/api/call/Echo", "[1]"}, {"/api/call/Echo", `["x",1]`}, {"/api/call/Echo", "{}"},
	} {
		if code, _ := post(t, ts.URL+bad.path, s.Token, bad.body); code < 400 {
			t.Errorf("%s %s = %d, want an error", bad.path, bad.body, code)
		}
	}
}

func TestGuard(t *testing.T) {
	s, ts, _ := newTest(t)
	if code, _ := post(t, ts.URL+"/api/call/Nothing", "", "[]"); code != http.StatusUnauthorized {
		t.Errorf("no token = %d", code)
	}
	if code, _ := post(t, ts.URL+"/api/call/Nothing", "wrong", "[]"); code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d", code)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/call/Nothing", strings.NewReader("[]"))
	req.Header.Set("X-Merry-Token", s.Token)
	req.Host = "evil.example:8080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host = %d", resp.StatusCode)
	}
	// Static files need no token (they hold no data).
	resp, err = http.Get(ts.URL + "/index.html")
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("static file: %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestEvents(t *testing.T) {
	s, ts, _ := newTest(t)
	resp, err := http.Get(ts.URL + "/api/events?token=" + s.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); line != ": connected\n" {
		t.Fatalf("first line %q", line)
	}
	_, _ = r.ReadString('\n')
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.Emit("vm:changed", map[string]string{"status": "running"})
	}()
	line, _ := r.ReadString('\n')
	if line != `data: {"data":{"status":"running"},"name":"vm:changed"}`+"\n" {
		t.Fatalf("event line %q", line)
	}
}
