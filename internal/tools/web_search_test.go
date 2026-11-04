package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const searchFixture = `
<html><body><table>
<tr class="result-sponsored">
  <td>1.</td>
  <td><a rel="nofollow" href="https://duckduckgo.com/y.js?u3=https%3A%2F%2Fads.example%2F" class="result-link">Buy Go</a></td>
</tr>
<tr class="result-sponsored">
  <td></td>
  <td class="result-snippet">An advertisement.</td>
</tr>
<tr>
  <td>2.</td>
  <td><a rel="nofollow" href="https://go.dev/" class="result-link">The Go Programming Language</a></td>
</tr>
<tr>
  <td></td>
  <td class="result-snippet">Go is an open source programming language.</td>
</tr>
<tr>
  <td>3.</td>
  <td><a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2Fnet%2Fhttp&amp;rut=1" class="result-link">net/http</a></td>
</tr>
<tr>
  <td></td>
  <td class="result-snippet">HTTP client and server.</td>
</tr>
</table></body></html>
`

func TestWebSearchParsesResultsAndSkipsAds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("q") != "golang" {
			http.Error(w, "query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, searchFixture)
	}))
	t.Cleanup(srv.Close)

	previous := duckDuckGoURL
	duckDuckGoURL = srv.URL
	t.Cleanup(func() { duckDuckGoURL = previous })

	set := New(t.TempDir())
	got, err := set.Run(t.Context(), "web_search", map[string]any{"query": "golang"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"results: 2",
		"1. The Go Programming Language",
		"https://go.dev/",
		"Go is an open source programming language.",
		"",
		"2. net/http",
		"https://pkg.go.dev/net/http",
		"HTTP client and server.",
	}, "\n")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	got, err = set.Run(t.Context(), "web_search", map[string]any{
		"query": "golang",
		"limit": float64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "results: 1\n") || strings.Contains(got, "pkg.go.dev") {
		t.Fatalf("limit not applied: %q", got)
	}
}

func TestWebSearchRejectsEmptyQueryAndRemoteStatus(t *testing.T) {
	set := New(t.TempDir())
	_, err := set.Run(t.Context(), "web_search", map[string]any{"query": "  "})
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("err %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	previous := duckDuckGoURL
	duckDuckGoURL = srv.URL
	t.Cleanup(func() { duckDuckGoURL = previous })

	_, err = set.Run(t.Context(), "web_search", map[string]any{"query": "golang"})
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("err %v", err)
	}
}
