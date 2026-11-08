package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

const (
	defaultSearchResults = 8
	maxSearchResults     = 10
	maxSearchQueryRunes  = 400
	maxSearchPageBytes   = 2 << 20
	searchRetryDelay     = 300 * time.Millisecond
)

// duckDuckGoURL is DuckDuckGo's no-JS search form.
// The instant-answer API does not return organic web results.
var duckDuckGoURL = "https://lite.duckduckgo.com/lite/"

const duckDuckGoUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:128.0) Gecko/20100101 Firefox/128.0"

var webClient = &http.Client{Timeout: 30 * time.Second}

func init() {
	registerDetail("web_search", webSearchDetail)
}

func webSearchDetail(args map[string]any) string {
	query, _ := args["query"].(string)
	return fmt.Sprintf("query=%q", query)
}

func webSearchSchema() Schema {
	return Schema{
		Name:        "web_search",
		Description: "Search the web with DuckDuckGo. Returns titles, URLs, and snippets. Use web_fetch to read a result.",
		Parameters: objectSchema(
			map[string]any{
				"query": stringProp("Search query."),
				"limit": intProp("Maximum results to return. Defaults to 8. At most 10."),
			},
			[]string{"query"},
		),
	}
}

func (s *Set) webSearch(ctx context.Context, args map[string]any) (string, error) {
	query, ok, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}
	if utf8.RuneCountInString(query) > maxSearchQueryRunes {
		return "", fmt.Errorf("query is too long")
	}

	limit, err := intArg(args, "limit", defaultSearchResults)
	if err != nil {
		return "", err
	}
	if limit < 1 {
		limit = defaultSearchResults
	}
	if limit > maxSearchResults {
		limit = maxSearchResults
	}
	return searchWeb(ctx, webClient, duckDuckGoURL, query, limit)
}

type searchHit struct {
	title   string
	url     string
	snippet string
}

func searchWeb(ctx context.Context, client *http.Client, endpoint, query string, limit int) (string, error) {
	page, err := fetchSearchPage(ctx, client, endpoint, query)
	if err != nil {
		return "", err
	}
	hits, err := parseSearchPage(page, limit)
	if err != nil {
		return "", err
	}
	return formatSearch(hits), nil
}

func fetchSearchPage(ctx context.Context, client *http.Client, endpoint, query string) ([]byte, error) {
	var lastStatus int
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(searchRetryDelay):
			}
		}

		body, status, err := postSearch(ctx, client, endpoint, query)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return body, nil
		}
		lastStatus = status
		retry := status == http.StatusAccepted || status == http.StatusTooManyRequests
		if !retry {
			return nil, fmt.Errorf("duckduckgo status %d", status)
		}
	}
	return nil, fmt.Errorf("duckduckgo status %d", lastStatus)
}

func postSearch(ctx context.Context, client *http.Client, endpoint, query string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("q", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", duckDuckGoUA)
	req.Header.Set("Referer", "https://lite.duckduckgo.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("duckduckgo: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchPageBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("duckduckgo: %w", err)
	}
	if len(payload) > maxSearchPageBytes {
		return nil, resp.StatusCode, fmt.Errorf("duckduckgo response is too large")
	}
	return payload, resp.StatusCode, nil
}

func parseSearchPage(page []byte, limit int) ([]searchHit, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(page)))
	if err != nil {
		return nil, fmt.Errorf("duckduckgo page: %w", err)
	}

	hits := []searchHit{}
	doc.Find("a.result-link").Each(func(_ int, sel *goquery.Selection) {
		if len(hits) >= limit {
			return
		}
		if sel.Closest("tr.result-sponsored").Length() > 0 {
			return
		}
		href, _ := sel.Attr("href")
		link := directURL(href)
		title := oneLine(sel.Text())
		if link == "" || title == "" {
			return
		}
		snippet := oneLine(sel.Closest("tr").Next().Find("td.result-snippet").Text())
		hits = append(hits, searchHit{
			title:   title,
			url:     link,
			snippet: snippet,
		})
	})
	return hits, nil
}

func directURL(href string) string {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com") {
		raw := parsed.Query().Get("uddg")
		if raw == "" {
			raw = parsed.Query().Get("u3")
		}
		if raw == "" {
			return ""
		}
		inner, err := url.Parse(raw)
		if err != nil || (inner.Scheme != "http" && inner.Scheme != "https") || inner.Hostname() == "" {
			return ""
		}
		return inner.String()
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func formatSearch(hits []searchHit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "results: %d\n", len(hits))
	for i, hit := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d. %s\n%s\n", i+1, hit.title, hit.url)
		if hit.snippet != "" {
			b.WriteString(hit.snippet)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
