package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// WebSearchTool implements web search functionality using multiple search engines
type WebSearchTool struct { //nolint:revive
	config       map[string]interface{}
	client       *http.Client
	apiKeys      map[string]string
	webFetchTool *WebFetchTool
}

// NewWebSearchTool creates a new WebSearchTool instance
func NewWebSearchTool(config map[string]interface{}) *WebSearchTool {
	if config == nil {
		config = make(map[string]interface{})
	}

	// Configure HTTP client with config
	var timeout = 30 * time.Second // Default timeout
	if timeoutInt, ok := config["web_search_timeout"].(int); ok {
		timeout = time.Duration(timeoutInt) * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
	}

	// Extract API keys from config
	apiKeys := make(map[string]string)
	if keys, ok := config["api_keys"]; ok {
		// Handle different types of API key configurations
		switch v := keys.(type) {
		case map[string]string:
			apiKeys = v
		case map[string]interface{}:
			// Convert interface{} values to strings
			for key, value := range v {
				if str, ok := value.(string); ok {
					apiKeys[key] = str
				}
			}
		default:
			// Handle WebSearchAPIKeys struct by using reflection
			if apiKeyStruct := v; apiKeyStruct != nil {
				// Use reflection to extract fields
				val := reflect.ValueOf(apiKeyStruct)
				if val.Kind() == reflect.Struct {
					typ := val.Type()
					for i := 0; i < val.NumField(); i++ {
						field := val.Field(i)
						fieldType := typ.Field(i)
						if field.Kind() == reflect.String {
							fieldName := strings.ToLower(fieldType.Name)
							apiKeys[fieldName] = field.String()
						}
					}
				}
			}
		}
	}

	return &WebSearchTool{
		config:       config,
		client:       client,
		apiKeys:      apiKeys,
		webFetchTool: NewWebFetchTool(config),
	}
}

func (t *WebSearchTool) Name() string { //nolint:revive
	return "web_search"
}

func (t *WebSearchTool) Description() string { //nolint:revive
	return "Search the web using multiple search engines with result aggregation"
}

func (t *WebSearchTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryWeb
}

func (t *WebSearchTool) RequiresConfirmation() bool { //nolint:revive
	return true // Require explicit confirmation for network operations
}

// ConcurrencySafe returns true: web searches are read-only network requests.
func (t *WebSearchTool) ConcurrencySafe() bool { return true }

func (t *WebSearchTool) Schema() *interfaces.ToolSchema { //nolint:revive
	queryProp := interfaces.NewStringProperty("Search query")
	queryProp.Examples = []string{"golang http client timeout", "最佳 Go 测试框架", "Rust vs Go performance"}
	queryProp.Usage = "Use concise keywords. Wrap exact phrases in quotes."

	engineProp := interfaces.NewStringPropertyWithEnum("Search engine to use", []string{"serper", "tavily", "duckduckgo", "auto"})
	engineProp.Examples = []string{"auto", "duckduckgo", "serper"}
	engineProp.Usage = "auto will pick an available engine (prefers Tavily/Serper if API keys configured)."

	maxResultsProp := interfaces.NewNumberProperty("Maximum number of results to return (default: 10)")
	maxResultsProp.Examples = []string{"5", "10", "20"}
	maxResultsProp.Usage = "Must be positive. Actual cap may be limited by engine."

	countryProp := interfaces.NewStringProperty("Country code for localized results (e.g., 'us', 'uk')")
	countryProp.Examples = []string{"us", "de", "cn"}
	countryProp.Usage = "Two-letter code. Some engines may ignore this."

	languageProp := interfaces.NewStringProperty("Language code for results (e.g., 'en', 'zh')")
	languageProp.Examples = []string{"en", "zh", "de"}
	languageProp.Usage = "Two-letter ISO code. Some engines may ignore this."

	safeProp := interfaces.NewBooleanProperty("Enable safe search filtering")
	safeProp.Examples = []string{"true", "false"}
	safeProp.Usage = "When true, attempts to filter explicit content where supported."

	timeFilterProp := interfaces.NewStringPropertyWithEnum("Time filter for results", []string{"day", "week", "month", "year"})
	timeFilterProp.Examples = []string{"day", "week"}
	timeFilterProp.Usage = "Limit results to a recent time window where supported. Omit to disable."

	snippetsProp := interfaces.NewBooleanProperty("Include content snippets in results")
	snippetsProp.Examples = []string{"true", "false"}
	snippetsProp.Usage = "If true, include short previews in results when provided by engine."

	fetchProp := interfaces.NewBooleanProperty("Fetch the full content of search results")
	fetchProp.Examples = []string{"false", "true"}
	fetchProp.Usage = "When true, additionally retrieves each result's page content using web_fetch (slower)."

	return interfaces.CreateSchema(
		"Search the web using multiple search engines",
		map[string]*interfaces.PropertySchema{
			"query":            queryProp,
			"engine":           engineProp,
			"max_results":      maxResultsProp,
			"country":          countryProp,
			"language":         languageProp,
			"safe_search":      safeProp,
			"time_filter":      timeFilterProp,
			"include_snippets": snippetsProp,
			"fetch_results":    fetchProp,
		},
		[]string{"query"},
	)
}

type SearchResult struct { //nolint:revive
	Title      string `json:"title"`
	URL        string `json:"url"`
	Snippet    string `json:"snippet"`
	DisplayURL string `json:"display_url"`
	Position   int    `json:"position"`
	Source     string `json:"source"`
	Timestamp  string `json:"timestamp,omitempty"`
}

type WebSearchResult struct { //nolint:revive
	Query         string         `json:"query"`
	Engine        string         `json:"engine"`
	Results       []SearchResult `json:"results"`
	TotalResults  int            `json:"total_results"`
	SearchTime    time.Duration  `json:"search_time"`
	Success       bool           `json:"success"`
	Error         string         `json:"error,omitempty"`
	NextPageToken string         `json:"next_page_token,omitempty"`
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool parameters are missing",
		}, nil
	}

	start := time.Now()

	// Extract parameters
	query, ok := params["query"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "query parameter is required and must be a string",
		}, nil
	}

	if strings.TrimSpace(query) == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "query cannot be empty",
		}, nil
	}

	// Get optional parameters
	engine := "auto"
	if engineParam, ok := params["engine"].(string); ok && engineParam != "" {
		engine = engineParam
	}

	// Get max results from config
	var maxResults = 10 // Default value
	if maxConfig, ok := t.config["web_search_max_results"].(int); ok && maxConfig > 0 {
		maxResults = maxConfig
	}
	if maxParam, ok := params["max_results"]; ok {
		if maxFloat, ok := maxParam.(float64); ok && maxFloat > 0 {
			maxResults = int(maxFloat)
		}
	}

	country := "us"
	if countryParam, ok := params["country"].(string); ok && countryParam != "" {
		country = countryParam
	}

	language := "en"
	if langParam, ok := params["language"].(string); ok && langParam != "" {
		language = langParam
	}

	safeSearch := true
	if safeParam, ok := params["safe_search"]; ok {
		safeSearch, _ = safeParam.(bool)
	}

	timeFilter := ""
	if timeParam, ok := params["time_filter"].(string); ok {
		timeFilter = timeParam
	}

	includeSnippets := true
	if snippetsParam, ok := params["include_snippets"]; ok {
		includeSnippets, _ = snippetsParam.(bool)
	}

	fetchResults := false
	if fetchParam, ok := params["fetch_results"]; ok {
		fetchResults, _ = fetchParam.(bool)
	}

	// Perform search
	result, err := t.performSearch(ctx, query, engine, maxResults, country, language, safeSearch, timeFilter, includeSnippets)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Search failed: %v", err),
		}, nil
	}

	result.SearchTime = time.Since(start)

	// Prepare metadata
	metadata := map[string]interface{}{
		"query":            query,
		"engine":           result.Engine,
		"max_results":      maxResults,
		"country":          country,
		"language":         language,
		"safe_search":      safeSearch,
		"time_filter":      timeFilter,
		"include_snippets": includeSnippets,
		"search_time":      result.SearchTime,
		"total_results":    result.TotalResults,
	}

	// Fetch full content if requested
	if fetchResults && result.Success && len(result.Results) > 0 {
		t.fetchFullContent(ctx, result)
	}

	// Format content for display
	userContent := t.formatForUser(result, metadata)
	llmContentRaw := t.formatForLLM(result, metadata)

	// Wrap LLM content with isolation tags for search results
	llmContent := wrapSearchContentForLLM(llmContentRaw, query)

	return &interfaces.ToolResult{
		Success:     result.Success,
		Data:        result,
		Metadata:    metadata,
		LLMContent:  llmContent,
		UserContent: userContent,
	}, nil
}

func (t *WebSearchTool) performSearch(ctx context.Context, query, engine string, maxResults int, country, language string, safeSearch bool, timeFilter string, includeSnippets bool) (*WebSearchResult, error) { //nolint:revive
	result := &WebSearchResult{
		Query:   query,
		Engine:  engine,
		Results: []SearchResult{},
	}

	// Choose search engine
	var searchFunc func(context.Context, string, int, map[string]string) (*WebSearchResult, error)

	switch engine {
	case "serper":
		searchFunc = t.searchSerper
	case "tavily":
		searchFunc = t.searchTavily
	case "duckduckgo":
		searchFunc = t.searchDuckDuckGo
	case "auto":
		// Try engines in order of preference: tavily, serper, duckduckgo
		engines := []string{"tavily", "serper", "duckduckgo"}
		for _, eng := range engines {
			if eng == "tavily" && t.apiKeys["tavily"] == "" {
				continue
			}
			if eng == "serper" && t.apiKeys["serper"] == "" {
				continue
			}

			var tempResult *WebSearchResult
			var err error

			switch eng {
			case "tavily":
				tempResult, err = t.searchTavily(ctx, query, maxResults, t.buildSearchParams(country, language, safeSearch, timeFilter))
			case "serper":
				tempResult, err = t.searchSerper(ctx, query, maxResults, t.buildSearchParams(country, language, safeSearch, timeFilter))
			case "duckduckgo":
				tempResult, err = t.searchDuckDuckGo(ctx, query, maxResults, t.buildSearchParams(country, language, safeSearch, timeFilter))
			}

			if err == nil && tempResult.Success {
				result = tempResult
				result.Engine = eng
				break
			} else { //nolint:revive
				// Debug logging
				logger.Warnf("Search engine %s failed: %v", eng, err)
				if tempResult != nil && tempResult.Error != "" {
					logger.Warnf("Search result error: %s", tempResult.Error)
				}
			}
		}

		if len(result.Results) == 0 {
			result.Error = "All search engines failed"
			return result, nil
		}
	default:
		result.Error = fmt.Sprintf("Unsupported search engine: %s", engine)
		return result, nil
	}

	if searchFunc != nil {
		searchResult, err := searchFunc(ctx, query, maxResults, t.buildSearchParams(country, language, safeSearch, timeFilter))
		if err != nil {
			result.Error = err.Error()
			return result, nil
		}
		result = searchResult
		result.Engine = engine
	}

	result.Success = true
	return result, nil
}

func (t *WebSearchTool) buildSearchParams(country, language string, safeSearch bool, timeFilter string) map[string]string {
	params := map[string]string{
		"country":     country,
		"language":    language,
		"time_filter": timeFilter,
	}

	if safeSearch {
		params["safe_search"] = "true"
	}

	return params
}

func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, maxResults int, params map[string]string) (*WebSearchResult, error) {
	result := &WebSearchResult{
		Query:   query,
		Engine:  "duckduckgo",
		Results: []SearchResult{},
	}

	// DuckDuckGo Instant Answer API
	baseURL := "https://api.duckduckgo.com/"

	// Build request URL with parameters
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("q", query)
	q.Add("format", "json")
	q.Add("no_html", "1")
	q.Add("skip_disambig", "1")

	// Add safe search if enabled
	if params["safe_search"] == "true" {
		q.Add("safe_search", "strict")
	}

	req.URL.RawQuery = q.Encode()

	// Set headers
	req.Header.Set("User-Agent", "nano-agent/1.0")
	req.Header.Set("Accept", "application/json")

	// Make request
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var ddgResponse struct {
		Abstract      string `json:"Abstract"`
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Answer        string `json:"Answer"`
		AnswerType    string `json:"AnswerType"`
		Definition    string `json:"Definition"`
		DefinitionURL string `json:"DefinitionURL"`
		Heading       string `json:"Heading"`
		Image         string `json:"Image"`
		Redirect      string `json:"Redirect"`
		RelatedTopics []struct {
			FirstURL string `json:"FirstURL"`
			Icon     struct {
				URL string `json:"URL"`
			} `json:"Icon"`
			Result string `json:"Result"`
			Text   string `json:"Text"`
		} `json:"RelatedTopics"`
		Results []struct {
			FirstURL string `json:"FirstURL"`
			Icon     struct {
				URL string `json:"URL"`
			} `json:"Icon"`
			Result string `json:"Result"`
			Text   string `json:"Text"`
		} `json:"Results"`
		Type string `json:"Type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ddgResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	position := 1

	// Add instant answer if available
	if ddgResponse.Answer != "" {
		result.Results = append(result.Results, SearchResult{
			Title:      ddgResponse.Heading,
			URL:        ddgResponse.AbstractURL,
			Snippet:    ddgResponse.Answer,
			DisplayURL: ddgResponse.AbstractURL,
			Position:   position,
			Source:     "duckduckgo",
		})
		position++
	}

	// Add abstract if available
	if ddgResponse.AbstractText != "" && ddgResponse.AbstractURL != "" {
		result.Results = append(result.Results, SearchResult{
			Title:      ddgResponse.Heading,
			URL:        ddgResponse.AbstractURL,
			Snippet:    ddgResponse.AbstractText,
			DisplayURL: ddgResponse.AbstractURL,
			Position:   position,
			Source:     "duckduckgo",
		})
		position++
	}

	// Add definition if available
	if ddgResponse.Definition != "" && ddgResponse.DefinitionURL != "" {
		result.Results = append(result.Results, SearchResult{
			Title:      "Definition: " + ddgResponse.Heading,
			URL:        ddgResponse.DefinitionURL,
			Snippet:    ddgResponse.Definition,
			DisplayURL: ddgResponse.DefinitionURL,
			Position:   position,
			Source:     "duckduckgo",
		})
		position++
	}

	// Add related topics (limited by maxResults)
	for _, topic := range ddgResponse.RelatedTopics {
		if len(result.Results) >= maxResults {
			break
		}
		if topic.FirstURL != "" && topic.Text != "" {
			result.Results = append(result.Results, SearchResult{
				Title:      topic.Text,
				URL:        topic.FirstURL,
				Snippet:    topic.Text,
				DisplayURL: topic.FirstURL,
				Position:   position,
				Source:     "duckduckgo",
			})
			position++
		}
	}

	// Add direct results
	for _, res := range ddgResponse.Results {
		if len(result.Results) >= maxResults {
			break
		}
		if res.FirstURL != "" && res.Text != "" {
			result.Results = append(result.Results, SearchResult{
				Title:      res.Text,
				URL:        res.FirstURL,
				Snippet:    res.Text,
				DisplayURL: res.FirstURL,
				Position:   position,
				Source:     "duckduckgo",
			})
			position++
		}
	}

	// If no results found, provide a basic fallback
	if len(result.Results) == 0 {
		logger.Warn("DuckDuckGo API returned no results, providing fallback message")
		result.Results = append(result.Results, SearchResult{
			Title:      "No results found",
			URL:        "https://duckduckgo.com/?q=" + strings.ReplaceAll(query, " ", "+"),
			Snippet:    "No instant answers or related topics found for this query. Try searching directly on DuckDuckGo.",
			DisplayURL: "duckduckgo.com",
			Position:   1,
			Source:     "duckduckgo",
		})
	}

	result.TotalResults = len(result.Results)
	result.Success = true
	return result, nil
}

func (t *WebSearchTool) searchSerper(ctx context.Context, query string, maxResults int, params map[string]string) (*WebSearchResult, error) {
	result := &WebSearchResult{
		Query:   query,
		Engine:  "serper",
		Results: []SearchResult{},
	}

	// Serper requires API key
	apiKey, ok := t.apiKeys["serper"]
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("Serper search requires API key") //nolint:staticcheck
	}

	// Validate and set max results for Serper (must be positive)
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 100 { // Serper typical limit
		maxResults = 100
	}

	// Build Serper API request
	baseURL := "https://google.serper.dev/search"
	reqBody := map[string]interface{}{
		"q":   query,
		"num": maxResults,
	}

	// Add optional parameters
	if params["country"] != "" {
		reqBody["gl"] = params["country"]
	}
	if params["language"] != "" {
		reqBody["hl"] = params["language"]
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Make request
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nano/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		// Read response body for better error details
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("Serper API returned status %d: %s", resp.StatusCode, string(body[:n])) //nolint:staticcheck
	}

	// Parse response
	var apiResponse struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
		SearchInformation struct {
			TotalResults string `json:"totalResults"`
		} `json:"searchInformation"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, err
	}

	// Convert API response to search results
	for i, item := range apiResponse.Organic {
		if i >= maxResults {
			break
		}

		result.Results = append(result.Results, SearchResult{
			Title:      item.Title,
			URL:        item.Link,
			Snippet:    item.Snippet,
			DisplayURL: item.Link,
			Position:   i + 1,
			Source:     "serper",
		})
	}

	result.TotalResults = len(result.Results)
	result.Success = true
	return result, nil
}

func (t *WebSearchTool) searchTavily(ctx context.Context, query string, maxResults int, params map[string]string) (*WebSearchResult, error) { //nolint:revive
	result := &WebSearchResult{
		Query:   query,
		Engine:  "tavily",
		Results: []SearchResult{},
	}

	// Tavily requires API key
	apiKey, ok := t.apiKeys["tavily"]
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("Tavily search requires API key") //nolint:staticcheck
	}

	// Validate and set max results for Tavily (must be between 1-20)
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}

	// Build Tavily API request
	baseURL := "https://api.tavily.com/search"
	reqBody := map[string]interface{}{
		"api_key":             apiKey,
		"query":               query,
		"max_results":         maxResults,
		"search_depth":        "basic",
		"include_answer":      false,
		"include_images":      false,
		"include_raw_content": false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Make request
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nano/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		// Read response body for better error details
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("Tavily API returned status %d: %s", resp.StatusCode, string(body[:n])) //nolint:staticcheck
	}

	// Parse response
	var apiResponse struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, err
	}

	// Convert API response to search results
	for i, item := range apiResponse.Results {
		if i >= maxResults {
			break
		}

		result.Results = append(result.Results, SearchResult{
			Title:      item.Title,
			URL:        item.URL,
			Snippet:    item.Content,
			DisplayURL: item.URL,
			Position:   i + 1,
			Source:     "tavily",
		})
	}

	result.TotalResults = len(result.Results)
	result.Success = true
	return result, nil
}

func (t *WebSearchTool) formatForUser(result *WebSearchResult, metadata map[string]interface{}) string { //nolint:revive
	var output strings.Builder

	output.WriteString("🔍 Web Search Results\n")
	output.WriteString("─────────────────────────────────────\n")
	fmt.Fprintf(&output, "🔎 Query: %s\n", result.Query)
	fmt.Fprintf(&output, "🌐 Engine: %s\n", result.Engine)
	fmt.Fprintf(&output, "⏱️  Search Time: %v\n", result.SearchTime)

	if result.Success {
		fmt.Fprintf(&output, "📊 Results: %d\n", len(result.Results))
		output.WriteString("─────────────────────────────────────\n")

		if len(result.Results) == 0 {
			output.WriteString("❌ No results found\n")
		} else {
			for i, res := range result.Results {
				if i >= 10 { // Limit display
					fmt.Fprintf(&output, "... and %d more results\n", len(result.Results)-i)
					break
				}

				fmt.Fprintf(&output, "\n%d. **%s**\n", res.Position, res.Title)
				fmt.Fprintf(&output, "   🔗 %s\n", res.URL)

				if res.Snippet != "" {
					snippet := res.Snippet
					if len(snippet) > 200 {
						snippet = snippet[:200] + "..."
					}
					fmt.Fprintf(&output, "   📝 %s\n", snippet)
				}
			}
		}
	} else {
		fmt.Fprintf(&output, "❌ Error: %s\n", result.Error)
	}

	return output.String()
}

func (t *WebSearchTool) fetchFullContent(ctx context.Context, result *WebSearchResult) {
	for i := range result.Results {
		// Use a new context for each fetch to avoid cancelling all on one error
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Validate the URL before fetching to guard against SSRF.
		u, err := url.Parse(result.Results[i].URL)
		if err != nil || validateURLForSSRF(u) != nil {
			continue
		}

		// Build a per-request client with SSRF-aware redirect checking.
		client := &http.Client{
			Timeout:   30 * time.Second,
			Transport: t.webFetchTool.client.Transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if err := validateURLForSSRF(req.URL); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		}

		fetchResult, err := t.webFetchTool.fetchContentWithClient(fetchCtx, client, result.Results[i].URL, "nano/1.0", 100000, true)
		if err == nil && fetchResult.Success {
			result.Results[i].Snippet = fetchResult.Content
		}
	}
}

func (t *WebSearchTool) formatForLLM(result *WebSearchResult, metadata map[string]interface{}) string { //nolint:revive
	var output strings.Builder

	fmt.Fprintf(&output, "Web search query: %s\n", result.Query)
	fmt.Fprintf(&output, "Search engine: %s\n", result.Engine)
	fmt.Fprintf(&output, "Search time: %v\n", result.SearchTime)

	if result.Success {
		fmt.Fprintf(&output, "Total results: %d\n\n", len(result.Results))

		if len(result.Results) == 0 {
			output.WriteString("No results found\n")
		} else {
			output.WriteString("Search Results:\n")
			for _, res := range result.Results {
				fmt.Fprintf(&output, "\n%d. %s\n", res.Position, res.Title)
				fmt.Fprintf(&output, "URL: %s\n", res.URL)

				if res.Snippet != "" {
					fmt.Fprintf(&output, "Snippet: %s\n", res.Snippet)
				}
			}
		}
	} else {
		fmt.Fprintf(&output, "Search failed: %s\n", result.Error)
	}

	return output.String()
}

// wrapSearchContentForLLM wraps search result content with isolation tags
func wrapSearchContentForLLM(content, query string) string {
	// Escape query for safe XML attribute usage
	escapedQuery := html.EscapeString(query)
	return fmt.Sprintf("<external_data source=\"search:%s\" type=\"search\">\n%s\n</external_data>", escapedQuery, content)
}
