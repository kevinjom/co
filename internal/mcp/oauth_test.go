package mcp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

func TestLoadClientID(t *testing.T) {
	home := t.TempDir()
	path := HomePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `
github:
  url: https://example.com/mcp
  client_id: co
  client_secret: secret
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ClientID != "co" || servers[0].ClientSecret != "secret" {
		t.Fatalf("servers %#v", servers)
	}
}

func TestOAuthSkippedWhenAuthorizationSet(t *testing.T) {
	withHeader := New(t.TempDir(), []Server{{
		Name:    "github",
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"authorization": "Bearer x"},
	}})
	if !hasAuthorization(withHeader.rows["github"].spec.Headers) {
		t.Fatal("expected authorization header")
	}
	plain := New(t.TempDir(), []Server{{Name: "wiki", URL: "https://example.com/mcp"}})
	handler, err := plain.rows["wiki"].oauthHandler()
	if err != nil || handler == nil {
		t.Fatal(err)
	}
	again, err := plain.rows["wiki"].oauthHandler()
	if err != nil || again != handler {
		t.Fatal("handler was recreated")
	}
}

func TestCallbackHandler(t *testing.T) {
	events := make(chan callbackEvent, 1)
	handler := callbackHandler(events)
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc&state=st&iss=https://as.example", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	ev := <-events
	if ev.err != nil || ev.result.Code != "abc" || ev.result.State != "st" || ev.result.Iss != "https://as.example" {
		t.Fatalf("event %#v", ev)
	}

	req = httptest.NewRequest(http.MethodGet, "/callback?error=access_denied", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	ev = <-events
	if ev.err == nil || !strings.Contains(ev.err.Error(), "access_denied") {
		t.Fatalf("event %#v", ev)
	}
}

func TestWaitOnReceivesCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	prev := browserOpener
	browserOpener = func(rawURL string) error {
		if rawURL != "https://as.example/auth" {
			t.Errorf("url %s", rawURL)
		}
		resp, err := http.Get("http://" + ln.Addr().String() + "/callback?code=abc&state=st&iss=https://as.example")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
	t.Cleanup(func() { browserOpener = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := waitOn(ctx, ln, "https://as.example/auth")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "abc" || res.State != "st" || res.Iss != "https://as.example" {
		t.Fatalf("result %#v", res)
	}
}

func TestWaitOnReportsBrowserFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	prev := browserOpener
	browserOpener = func(string) error { return errors.New("no display") }
	t.Cleanup(func() { browserOpener = prev })

	_, err = waitOn(context.Background(), ln, "https://as.example/auth?x=1")
	if err == nil || !strings.Contains(err.Error(), "https://as.example/auth?x=1") {
		t.Fatal(err)
	}
}

func TestBusyCallbackPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	prev := callbackListen
	callbackListen = ln.Addr().String()
	t.Cleanup(func() { callbackListen = prev })
	_, err = waitForCode(context.Background(), "https://as.example/auth")
	if err == nil || !strings.Contains(err.Error(), "oauth callback") {
		t.Fatal(err)
	}
}

func TestTokenFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	store := openTokenStore(home)
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	cfg := &oauth2.Config{
		ClientID:     "cid",
		ClientSecret: "sec",
		Endpoint: oauth2.Endpoint{
			AuthURL:   "https://as.example/auth",
			TokenURL:  "https://as.example/token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: []string{"read"},
	}
	tok := &oauth2.Token{
		AccessToken:  "access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       expiry,
	}
	src, err := store.wrap(context.Background(), "wiki", "https://as.example", cfg, tok)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(tokenPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"auth_style": "post"`) {
		t.Fatalf("file %s", data)
	}
	got, err := src.Token()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access" {
		t.Fatalf("token %q", got.AccessToken)
	}

	loaded := openTokenStore(home)
	if id, secret, ok := loaded.client("wiki"); !ok || id != "cid" || secret != "sec" {
		t.Fatalf("client %q %q %v", id, secret, ok)
	}
	again := loaded.source("wiki", "")
	got, err = again.Token()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access" || got.RefreshToken != "refresh" {
		t.Fatalf("reloaded %#v", got)
	}
	if loaded.source("wiki", "other-client") != nil {
		t.Fatal("token reused for a different client")
	}
}

func TestAnnotateAuthError(t *testing.T) {
	err := annotateAuthError(errors.New("no configured client registration methods are supported by the authorization server"))
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatal(err)
	}
	if annotateAuthError(nil) != nil {
		t.Fatal("nil")
	}
}

func TestFetchCodeRequiresURL(t *testing.T) {
	hub := New(t.TempDir(), nil)
	_, err := hub.fetchCode(context.Background(), &auth.AuthorizationArgs{})
	if err == nil {
		t.Fatal("expected error")
	}
}
