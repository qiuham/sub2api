package apicompat

import (
	"encoding/json"
	"net/url"
)

// webSearchResults exposes actual upstream sources to Messages clients.
// OpenAI supplies neither Anthropic encrypted_content nor page snippets;
// do not invent either. Empty sources remain an honest empty list.
func webSearchResults(action *WebSearchAction) json.RawMessage {
	type result struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	results := make([]result, 0)
	seen := make(map[string]bool)
	if action != nil {
		for _, source := range action.Sources {
			u, err := url.Parse(source.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || seen[source.URL] {
				continue
			}
			seen[source.URL] = true
			title := source.Title
			if title == "" {
				title = source.URL
			}
			results = append(results, result{Type: "web_search_result", URL: source.URL, Title: title})
		}
	}
	raw, _ := json.Marshal(results)
	return raw
}
