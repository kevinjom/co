package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchReturnsMarkdown(t *testing.T) {
	const page = `<!doctype html>
<html>
<head><title>Widget Notes</title><meta charset="utf-8"></head>
<body>
<nav>Home</nav>
<article>
<h1>Widget Notes</h1>
<p>Widgets are assembled from two brass pins and a short spring.</p>
<p>The spring sits between the pins and keeps the latch closed.</p>
<p>A third paragraph gives the extractor enough article text to keep.</p>
</article>
</body>
</html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)

	set := New(t.TempDir())
	got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "url: "+srv.URL) || !strings.Contains(got, "title: Widget Notes") {
		t.Fatalf("header missing: %q", got)
	}
	if !strings.Contains(got, "Widgets are assembled from two brass pins") {
		t.Fatalf("body missing: %q", got)
	}
	if strings.Contains(got, "truncated: true") {
		t.Fatalf("short page truncated: %q", got)
	}
}

func TestWebFetchRejectsMissingAndNonHTTP(t *testing.T) {
	set := New(t.TempDir())
	_, err := set.Run(t.Context(), "web_fetch", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("err %v", err)
	}
	_, err = set.Run(t.Context(), "web_fetch", map[string]any{"url": "file:///etc/passwd"})
	if err == nil {
		t.Fatal("file url accepted")
	}
}

func TestWebFetchTruncatesLongPages(t *testing.T) {
	body := strings.Repeat("Widgets stay in service for years. ", 2000)
	page := "<!doctype html><html><head><title>Long</title></head><body><article><p>" + body + "</p></article></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)

	set := New(t.TempDir())
	got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL + "/long"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "truncated: true") {
		t.Fatalf("not truncated: %q", got[:200])
	}
	if len(got) > maxFetchBytes+512 {
		t.Fatalf("output length %d", len(got))
	}
}
