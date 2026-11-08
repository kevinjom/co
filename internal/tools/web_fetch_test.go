package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	widgetPage = `<!doctype html>
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

	plainPage = `<!doctype html><html><head></head><body><article><p>Only a paragraph.</p></article></body></html>`

	emptyPage = `<!doctype html><html><head><title>Empty</title></head><body></body></html>`
)

func TestWebFetchSchema(t *testing.T) {
	t.Parallel()

	schema := webFetchSchema()
	if schema.Name != "web_fetch" {
		t.Fatalf("schema.Name = %q, want %q", schema.Name, "web_fetch")
	}
	const wantDescription = "Fetch one http or https page and return its main content as Markdown."
	if schema.Description != wantDescription {
		t.Fatalf("schema.Description = %q, want %q", schema.Description, wantDescription)
	}

	params := schema.Parameters
	if params["type"] != "object" {
		t.Fatalf("params[type] = %v, want object", params["type"])
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("params[required] is not []string: %T", params["required"])
	}
	if len(required) != 1 || required[0] != "url" {
		t.Fatalf("required = %v, want [url]", required)
	}

	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("params[properties] is not map[string]any: %T", params["properties"])
	}
	url, ok := properties["url"].(map[string]any)
	if !ok {
		t.Fatalf("url property is not map[string]any: %T", properties["url"])
	}
	if url["type"] != "string" {
		t.Fatalf("url.type = %v, want string", url["type"])
	}
	const wantURLDescription = "Absolute http or https URL to read."
	if url["description"] != wantURLDescription {
		t.Fatalf("url.description = %v, want %q", url["description"], wantURLDescription)
	}
}

func TestWebFetch(t *testing.T) {
	t.Parallel()

	t.Run("returns the extracted article", func(t *testing.T) {
		t.Parallel()

		srv := serveHTML(t, widgetPage)
		set := New(t.TempDir())
		got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL})
		if err != nil {
			t.Fatalf("web_fetch returned error: %v", err)
		}

		want := strings.Join([]string{
			"url: " + srv.URL,
			"title: Widget Notes",
			"words: 31",
			"",
			"<article>" +
				"<p>Widgets are assembled from two brass pins and a short spring.</p>" +
				"<p>The spring sits between the pins and keeps the latch closed.</p>" +
				"<p>A third paragraph gives the extractor enough article text to keep.</p>" +
				"</article>",
		}, "\n")
		if got != want {
			t.Fatalf("web_fetch = %q, want %q", got, want)
		}
	})

	t.Run("omits a blank title", func(t *testing.T) {
		t.Parallel()

		srv := serveHTML(t, plainPage)
		set := New(t.TempDir())
		got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL})
		if err != nil {
			t.Fatalf("web_fetch returned error: %v", err)
		}

		want := strings.Join([]string{
			"url: " + srv.URL,
			"words: 3",
			"",
			"<body><p>Only a paragraph.</p></body>",
		}, "\n")
		if got != want {
			t.Fatalf("web_fetch = %q, want %q", got, want)
		}
	})

	t.Run("trims the url", func(t *testing.T) {
		t.Parallel()

		srv := serveHTML(t, plainPage)
		set := New(t.TempDir())
		got, err := set.Run(t.Context(), "web_fetch", map[string]any{
			"url": " \t" + srv.URL + "\n",
		})
		if err != nil {
			t.Fatalf("web_fetch returned error: %v", err)
		}

		want := strings.Join([]string{
			"url: " + srv.URL,
			"words: 3",
			"",
			"<body><p>Only a paragraph.</p></body>",
		}, "\n")
		if got != want {
			t.Fatalf("web_fetch = %q, want %q", got, want)
		}
	})

	t.Run("keeps an empty document", func(t *testing.T) {
		t.Parallel()

		srv := serveHTML(t, emptyPage)
		set := New(t.TempDir())
		got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL})
		if err != nil {
			t.Fatalf("web_fetch returned error: %v", err)
		}

		want := strings.Join([]string{
			"url: " + srv.URL,
			"title: Empty",
			"words: 0",
			"",
			"<body></body>",
		}, "\n")
		if got != want {
			t.Fatalf("web_fetch = %q, want %q", got, want)
		}
	})

	t.Run("truncates a long page", func(t *testing.T) {
		t.Parallel()

		const sentence = "Widgets stay in service for years. "
		article := strings.Repeat(sentence, 2000)
		page := "<!doctype html><html><head><title>Long</title></head><body><article><p>" +
			article +
			"</p></article></body></html>"
		srv := serveHTML(t, page)
		pageURL := srv.URL + "/long"

		set := New(t.TempDir())
		got, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": pageURL})
		if err != nil {
			t.Fatalf("web_fetch returned error: %v", err)
		}

		head, body, ok := strings.Cut(got, "\n\n")
		if !ok {
			t.Fatalf("web_fetch = %q, want a body after the header", got)
		}
		wantHead := strings.Join([]string{
			"url: " + pageURL,
			"title: Long",
			"words: 12000",
			"truncated: true",
		}, "\n")
		if head != wantHead {
			t.Fatalf("header = %q, want %q", head, wantHead)
		}
		if len(body) > maxFetchBytes {
			t.Fatalf("body length %d exceeds %d", len(body), maxFetchBytes)
		}
		if !strings.Contains(body, sentence) {
			t.Fatalf("body missing opening text: %q", body)
		}
		if strings.Contains(got, article) {
			t.Fatal("full article was returned")
		}
	})

	t.Run("rejects a bad url", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			args    map[string]any
			wantErr string
		}{
			{
				name:    "missing",
				args:    map[string]any{},
				wantErr: "url is required",
			},
			{
				name:    "empty",
				args:    map[string]any{"url": ""},
				wantErr: "url is required",
			},
			{
				name:    "whitespace",
				args:    map[string]any{"url": "  "},
				wantErr: "url is required",
			},
			{
				name:    "not a string",
				args:    map[string]any{"url": 12},
				wantErr: "url: expected a string",
			},
			{
				name:    "file scheme",
				args:    map[string]any{"url": "file:///etc/passwd"},
				wantErr: "URL must be absolute HTTP(S)",
			},
			{
				name:    "relative",
				args:    map[string]any{"url": "/etc/passwd"},
				wantErr: "URL must be absolute HTTP(S)",
			},
			{
				name:    "missing host",
				args:    map[string]any{"url": "http://"},
				wantErr: "URL must be absolute HTTP(S)",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				set := New(t.TempDir())
				_, err := set.Run(t.Context(), "web_fetch", tt.args)
				if err == nil {
					t.Fatalf("web_fetch(%#v) returned nil error", tt.args)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			})
		}
	})

	t.Run("reports a remote failure", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			handler http.HandlerFunc
			wantErr string
		}{
			{
				name: "non-success status",
				handler: func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "nope", http.StatusBadGateway)
				},
				wantErr: "unexpected HTTP status",
			},
			{
				name: "non-html content type",
				handler: func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				},
				wantErr: "content is not HTML",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				srv := httptest.NewServer(tt.handler)
				t.Cleanup(srv.Close)

				set := New(t.TempDir())
				_, err := set.Run(t.Context(), "web_fetch", map[string]any{"url": srv.URL})
				if err == nil {
					t.Fatal("web_fetch returned nil error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			})
		}
	})
}

func TestFormatFetch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pageURL   string
		title     string
		words     int
		body      string
		truncated bool
		want      string
	}{
		{
			name:    "title and body",
			pageURL: "https://example.com/a",
			title:   "Notes",
			words:   2,
			body:    "Hello there.",
			want: strings.Join([]string{
				"url: https://example.com/a",
				"title: Notes",
				"words: 2",
				"",
				"Hello there.",
			}, "\n"),
		},
		{
			name:    "omits a blank title",
			pageURL: "https://example.com/a",
			words:   1,
			body:    "Hello.",
			want: strings.Join([]string{
				"url: https://example.com/a",
				"words: 1",
				"",
				"Hello.",
			}, "\n"),
		},
		{
			name:      "marks truncation before the body",
			pageURL:   "https://example.com/a",
			title:     "Long",
			words:     10,
			body:      "cut",
			truncated: true,
			want: strings.Join([]string{
				"url: https://example.com/a",
				"title: Long",
				"words: 10",
				"truncated: true",
				"",
				"cut",
			}, "\n"),
		},
		{
			name:    "omits an empty body",
			pageURL: "https://example.com/a",
			title:   "Empty",
			want: strings.Join([]string{
				"url: https://example.com/a",
				"title: Empty",
				"words: 0",
			}, "\n"),
		},
		{
			name:    "trims trailing newlines",
			pageURL: "https://example.com/a",
			words:   1,
			body:    "Hello.\n\n",
			want: strings.Join([]string{
				"url: https://example.com/a",
				"words: 1",
				"",
				"Hello.",
			}, "\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatFetch(
				tt.pageURL,
				tt.title,
				tt.words,
				tt.body,
				tt.truncated,
			)
			if got != tt.want {
				t.Fatalf("formatFetch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClipText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		maxBytes  int
		want      string
		truncated bool
	}{
		{
			name:     "shorter than the limit",
			text:     "abc",
			maxBytes: 4,
			want:     "abc",
		},
		{
			name:     "equal to the limit",
			text:     "abc",
			maxBytes: 3,
			want:     "abc",
		},
		{
			name:      "zero limit",
			text:      "abc",
			maxBytes:  0,
			want:      "",
			truncated: true,
		},
		{
			name:      "steps back to a rune boundary",
			text:      "ab界cd",
			maxBytes:  4,
			want:      "ab",
			truncated: true,
		},
		{
			name:      "breaks at the last line inside the window",
			text:      strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100),
			maxBytes:  150,
			want:      strings.Repeat("a", 100),
			truncated: true,
		},
		{
			name:      "keeps a line break older than the window",
			text:      "a\n" + strings.Repeat("b", 2000),
			maxBytes:  1500,
			want:      "a\n" + strings.Repeat("b", 1498),
			truncated: true,
		},
		{
			name:      "does not break on a leading newline",
			text:      "\n" + strings.Repeat("b", 20),
			maxBytes:  10,
			want:      "\n" + strings.Repeat("b", 9),
			truncated: true,
		},
		{
			name:      "drops trailing newlines at the break",
			text:      "hello\n\n" + strings.Repeat("z", 20),
			maxBytes:  8,
			want:      "hello",
			truncated: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, truncated := clipText(tt.text, tt.maxBytes)
			if got != tt.want || truncated != tt.truncated {
				t.Fatalf(
					"clipText(%q, %d) = %q, %t, want %q, %t",
					tt.text,
					tt.maxBytes,
					got,
					truncated,
					tt.want,
					tt.truncated,
				)
			}
		})
	}
}

func serveHTML(t *testing.T, page string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	return srv
}
