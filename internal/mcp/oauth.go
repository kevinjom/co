package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

const (
	oauthTimeout = 5 * time.Minute
	callbackHost = "127.0.0.1:8737"
	callbackPath = "/callback"
	callbackURL  = "http://127.0.0.1:8737/callback"
)

// callbackListen is the loopback address for the OAuth redirect. Tests point it at a taken port.
var callbackListen = callbackHost

// browserOpener launches the authorization URL. Tests replace it.
var browserOpener = openBrowser

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func hasAuthorization(headers map[string]string) bool {
	for key := range headers {
		if strings.EqualFold(key, "Authorization") {
			return true
		}
	}
	return false
}

func annotateAuthError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "no configured client registration methods") {
		return fmt.Errorf("%w; set client_id for this server in mcp.yaml", err)
	}
	return err
}

type oauthClient struct {
	inner *auth.AuthorizationCodeHandler
}

func (c oauthClient) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return c.inner.TokenSource(ctx)
}

func (c oauthClient) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	return annotateAuthError(c.inner.Authorize(ctx, req, resp))
}

func (r *row) oauthHandler() (auth.OAuthHandler, error) {
	if r.oauth != nil {
		return r.oauth, nil
	}
	if err := r.hub.tokens.err; err != nil {
		return nil, fmt.Errorf("reading mcp-tokens.json: %w", err)
	}
	handler, err := auth.NewAuthorizationCodeHandler(r.authConfig())
	if err != nil {
		return nil, err
	}
	r.oauth = oauthClient{inner: handler}
	return r.oauth, nil
}

func (r *row) authConfig() *auth.AuthorizationCodeHandlerConfig {
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: callbackURL,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			res, err := r.hub.fetchCode(ctx, args)
			if err != nil {
				return nil, err
			}
			r.pendingIssuer = res.Iss
			return res, nil
		},
		RequestRefreshToken:   true,
		AcceptUnadvertisedIss: true,
		Client:                &http.Client{Timeout: requestTimeout},
		NewTokenSource:        r.hub.tokens.saver(r.spec.Name, func() string { return r.pendingIssuer }),
		InitialTokenSource:    r.hub.tokens.source(r.spec.Name, r.spec.ClientID),
	}
	if r.spec.ClientID != "" {
		cfg.PreregisteredClient = clientCredentials(r.spec.ClientID, r.spec.ClientSecret)
		return cfg
	}
	if id, secret, ok := r.hub.tokens.client(r.spec.Name); ok {
		cfg.PreregisteredClient = clientCredentials(id, secret)
	}
	cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
		Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName:              "co",
			RedirectURIs:            []string{callbackURL},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
		},
	}
	return cfg
}

func clientCredentials(id, secret string) *oauthex.ClientCredentials {
	creds := &oauthex.ClientCredentials{ClientID: id}
	if secret != "" {
		creds.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: secret}
	}
	return creds
}

func (h *Hub) fetchCode(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	h.login.Lock()
	defer h.login.Unlock()
	if args == nil || args.URL == "" {
		return nil, errors.New("oauth: missing authorization url")
	}
	return waitForCode(ctx, args.URL)
}

func waitForCode(ctx context.Context, authURL string) (*auth.AuthorizationResult, error) {
	ln, err := net.Listen("tcp", callbackListen)
	if err != nil {
		return nil, fmt.Errorf("oauth callback %s: %w", callbackListen, err)
	}
	return waitOn(ctx, ln, authURL)
}

type callbackEvent struct {
	result *auth.AuthorizationResult
	err    error
}

func waitOn(ctx context.Context, ln net.Listener, authURL string) (*auth.AuthorizationResult, error) {
	defer ln.Close()
	events := make(chan callbackEvent, 1)
	srv := &http.Server{Handler: callbackHandler(events)}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()

	if err := browserOpener(authURL); err != nil {
		return nil, fmt.Errorf("open a browser at %s: %w", authURL, err)
	}
	select {
	case ev := <-events:
		if ev.err != nil {
			return nil, ev.err
		}
		return ev.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func callbackHandler(events chan<- callbackEvent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != callbackPath {
			http.NotFound(w, req)
			return
		}
		query := req.URL.Query()
		if msg := query.Get("error"); msg != "" {
			http.Error(w, "authorization failed", http.StatusBadRequest)
			sendCallback(events, callbackEvent{err: fmt.Errorf("oauth: %s", msg)})
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, "Signed in. You can close this window.\n")
		sendCallback(events, callbackEvent{result: &auth.AuthorizationResult{
			Code:  code,
			State: query.Get("state"),
			Iss:   query.Get("iss"),
		}})
	})
}

func sendCallback(events chan<- callbackEvent, ev callbackEvent) {
	select {
	case events <- ev:
	default:
	}
}

func tokenPath(home string) string {
	return filepath.Join(home, ".co", "mcp-tokens.json")
}

type tokenRecord struct {
	Issuer       string    `json:"issuer,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AuthURL      string    `json:"auth_url,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	AuthStyle    string    `json:"auth_style,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

type tokenFile struct {
	Servers map[string]tokenRecord `json:"servers"`
}

type tokenStore struct {
	mu      sync.Mutex
	path    string
	err     error
	servers map[string]tokenRecord
}

func openTokenStore(home string) *tokenStore {
	store := &tokenStore{servers: map[string]tokenRecord{}}
	if home == "" {
		return store
	}
	store.path = tokenPath(home)
	data, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store
		}
		store.err = err
		return store
	}
	var file tokenFile
	if err := json.Unmarshal(data, &file); err != nil {
		store.err = err
		return store
	}
	if file.Servers != nil {
		store.servers = file.Servers
	}
	return store
}

func (s *tokenStore) saver(server string, issuer func() string) func(context.Context, *oauth2.Config, *oauth2.Token) (oauth2.TokenSource, error) {
	return func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
		return s.wrap(ctx, server, issuer(), cfg, tok)
	}
}

func (s *tokenStore) wrap(ctx context.Context, server, issuer string, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
	if err := s.save(server, issuer, cfg, tok); err != nil {
		return nil, err
	}
	base := cfg.TokenSource(ctx, tok)
	return savingSource{base: base, save: func(next *oauth2.Token) {
		_ = s.save(server, issuer, cfg, next)
	}}, nil
}

func (s *tokenStore) source(server, wantClient string) oauth2.TokenSource {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	rec, ok := s.servers[server]
	s.mu.Unlock()
	if !ok || rec.AccessToken == "" {
		return nil
	}
	if wantClient != "" && rec.ClientID != "" && rec.ClientID != wantClient {
		return nil
	}
	tok := &oauth2.Token{
		AccessToken:  rec.AccessToken,
		RefreshToken: rec.RefreshToken,
		TokenType:    rec.TokenType,
		Expiry:       rec.Expiry,
	}
	cfg := &oauth2.Config{
		ClientID:     rec.ClientID,
		ClientSecret: rec.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   rec.AuthURL,
			TokenURL:  rec.TokenURL,
			AuthStyle: parseAuthStyle(rec.AuthStyle),
		},
		Scopes: rec.Scopes,
	}
	if rec.RefreshToken == "" || rec.TokenURL == "" || rec.ClientID == "" {
		return oauth2.ReuseTokenSource(tok, oauth2.StaticTokenSource(tok))
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: requestTimeout})
	return savingSource{base: cfg.TokenSource(ctx, tok), save: func(next *oauth2.Token) {
		_ = s.save(server, rec.Issuer, cfg, next)
	}}
}

func (s *tokenStore) client(server string) (string, string, bool) {
	if s == nil {
		return "", "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.servers[server]
	if !ok || rec.ClientID == "" {
		return "", "", false
	}
	return rec.ClientID, rec.ClientSecret, true
}

func (s *tokenStore) save(server, issuer string, cfg *oauth2.Config, tok *oauth2.Token) error {
	if tok == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.servers[server]
	if issuer != "" {
		rec.Issuer = issuer
	}
	if cfg != nil {
		if cfg.ClientID != "" {
			rec.ClientID = cfg.ClientID
		}
		if cfg.ClientSecret != "" {
			rec.ClientSecret = cfg.ClientSecret
		}
		if cfg.Endpoint.AuthURL != "" {
			rec.AuthURL = cfg.Endpoint.AuthURL
		}
		if cfg.Endpoint.TokenURL != "" {
			rec.TokenURL = cfg.Endpoint.TokenURL
		}
		if style := authStyleName(cfg.Endpoint.AuthStyle); style != "" {
			rec.AuthStyle = style
		}
		if len(cfg.Scopes) > 0 {
			rec.Scopes = append([]string(nil), cfg.Scopes...)
		}
	}
	rec.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		rec.RefreshToken = tok.RefreshToken
	}
	if tok.TokenType != "" {
		rec.TokenType = tok.TokenType
	}
	if !tok.Expiry.IsZero() {
		rec.Expiry = tok.Expiry.UTC()
	}
	s.servers[server] = rec
	if s.path == "" {
		return nil
	}
	return s.writeLocked()
}

func (s *tokenStore) writeLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(tokenFile{Servers: s.servers}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp, err := os.CreateTemp(dir, "mcp-tokens-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func authStyleName(style oauth2.AuthStyle) string {
	switch style {
	case oauth2.AuthStyleInParams:
		return "post"
	case oauth2.AuthStyleInHeader:
		return "header"
	default:
		return ""
	}
}

func parseAuthStyle(name string) oauth2.AuthStyle {
	switch name {
	case "post":
		return oauth2.AuthStyleInParams
	case "header":
		return oauth2.AuthStyleInHeader
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

type savingSource struct {
	base oauth2.TokenSource
	save func(*oauth2.Token)
}

func (s savingSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	if s.save != nil {
		s.save(tok)
	}
	return tok, nil
}
