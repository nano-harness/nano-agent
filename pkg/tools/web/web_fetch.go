package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	. "github.com/nano-harness/nano-agent/pkg/tools/filesystem" //nolint:revive,staticcheck
)

// WebFetchTool implements HTTP content retrieval and processing
type WebFetchTool struct { //nolint:revive
	config map[string]interface{}
	client *http.Client
}

// NewWebFetchTool creates a new WebFetchTool instance
func NewWebFetchTool(config map[string]interface{}) *WebFetchTool {
	if config == nil {
		config = make(map[string]interface{})
	}

	// Configure HTTP client with config
	var timeout = 30 * time.Second // Default timeout
	if timeoutInt, ok := config["web_request_timeout"].(int); ok {
		timeout = time.Duration(timeoutInt) * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}

	return &WebFetchTool{
		config: config,
		client: client,
	}
}

func (t *WebFetchTool) Name() string { //nolint:revive
	return "web_fetch"
}

func (t *WebFetchTool) Description() string { //nolint:revive
	return "Fetch and process content from web URLs with HTML to markdown conversion"
}

func (t *WebFetchTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryWeb
}

func (t *WebFetchTool) RequiresConfirmation() bool { //nolint:revive
	return true // Require explicit confirmation for network operations
}

// ConcurrencySafe returns true: fetching a URL is a read-only network request.
func (t *WebFetchTool) ConcurrencySafe() bool { return true }

func (t *WebFetchTool) Schema() *interfaces.ToolSchema { //nolint:revive
	urlProp := interfaces.NewStringProperty("URL to fetch content from")
	urlProp.Examples = []string{"https://example.com", "https://golang.org/doc/", "http://httpbin.org/html"}
	urlProp.Usage = "Only http/https are supported. Provide an absolute URL."

	followProp := interfaces.NewBooleanProperty("Follow HTTP redirects (default: true)")
	followProp.Examples = []string{"true", "false"}
	followProp.Usage = "When false, the first redirect response is returned without following."

	uaProp := interfaces.NewStringProperty("Custom User-Agent header")
	uaProp.Examples = []string{"nano-agent/1.0", "Mozilla/5.0 (compatible; Bot/1.0)"}
	uaProp.Usage = "Overrides default user agent. Can also be set via config."

	headersProp := interfaces.NewStringProperty("Additional HTTP headers (JSON format)")
	headersProp.Examples = []string{"{\"Accept-Language\":\"zh-CN\"}", "{\"Authorization\":\"Bearer TOKEN\"}"}
	headersProp.Usage = "Optional. Provide as JSON object string; currently not applied unless implemented in fetch path."

	maxLenProp := interfaces.NewNumberProperty("Maximum content length to fetch (bytes)")
	maxLenProp.Examples = []string{"50000", "200000"}
	maxLenProp.Usage = "If set, response body is capped to this many bytes."

	extractProp := interfaces.NewBooleanProperty("Extract only text content, removing HTML")
	extractProp.Examples = []string{"true", "false"}
	extractProp.Usage = "When true or when Content-Type is HTML, content is converted to markdown-like text."

	maxCharsProp := interfaces.NewNumberProperty("Maximum number of characters in the output content (0 for no limit)")
	maxCharsProp.Examples = []string{"0", "2000", "10000"}
	maxCharsProp.Usage = "After fetch, trims content length for display/consumption."

	promptProp := interfaces.NewStringProperty("AI prompt to process the fetched content")
	promptProp.Examples = []string{"Summarize the main points", "Extract all links"}
	promptProp.Usage = "If provided, sets processed_content to indicate requested AI processing (placeholder)."

	return interfaces.CreateSchema(
		"Fetch and process content from web URLs",
		map[string]*interfaces.PropertySchema{
			"url":                urlProp,
			"follow_redirects":   followProp,
			"user_agent":         uaProp,
			"headers":            headersProp,
			"max_content_length": maxLenProp,
			"extract_text_only":  extractProp,
			"max_output_chars":   maxCharsProp,
			"prompt":             promptProp,
		},
		[]string{"url"},
	)
}

type WebFetchResult struct { //nolint:revive
	URL              string            `json:"url"`
	FinalURL         string            `json:"final_url"`
	StatusCode       int               `json:"status_code"`
	ContentType      string            `json:"content_type"`
	ContentLength    int64             `json:"content_length"`
	Content          string            `json:"content"`
	ProcessedContent string            `json:"processed_content"`
	Headers          map[string]string `json:"headers"`
	RedirectChain    []string          `json:"redirect_chain"`
	ProcessingTime   time.Duration     `json:"processing_time"`
	Success          bool              `json:"success"`
	Error            string            `json:"error,omitempty"`
}

func (t *WebFetchTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool parameters are missing",
		}, nil
	}

	start := time.Now()

	// Extract parameters
	urlStr, ok := params["url"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "url parameter is required and must be a string",
		}, nil
	}

	// Validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid URL: %v", err),
		}, nil
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Only HTTP and HTTPS URLs are supported",
		}, nil
	}

	// Get optional parameters

	followRedirects := true
	if followParam, ok := params["follow_redirects"]; ok {
		followRedirects, _ = followParam.(bool)
	}

	// Get user agent from config or use default
	userAgent := "nano-agent/1.0"
	if uaConfig, ok := t.config["user_agent"].(string); ok && uaConfig != "" {
		userAgent = uaConfig
	}
	if uaParam, ok := params["user_agent"].(string); ok && uaParam != "" {
		userAgent = uaParam
	}

	// Get max content length from config
	var maxContentLength int64
	if maxConfig, ok := t.config["web_max_content_size"].(int); ok {
		maxContentLength = int64(maxConfig)
	}
	if maxParam, ok := params["max_content_length"]; ok {
		if maxFloat, ok := maxParam.(float64); ok {
			maxContentLength = int64(maxFloat)
		}
	}

	extractTextOnly := false
	if extractParam, ok := params["extract_text_only"]; ok {
		extractTextOnly, _ = extractParam.(bool)
	}

	maxOutputChars := 0
	if maxCharsParam, ok := params["max_output_chars"]; ok {
		if maxCharsFloat, ok := maxCharsParam.(float64); ok {
			maxOutputChars = int(maxCharsFloat)
		}
	}

	// Get AI prompt parameter
	prompt := ""
	if promptParam, ok := params["prompt"].(string); ok {
		prompt = promptParam
	}

	// Configure client for redirects
	if !followRedirects {
		t.client.CheckRedirect = func(req *http.Request, via []*http.Request) error { //nolint:revive
			return http.ErrUseLastResponse
		}
	}

	// Perform web fetch
	result, err := t.fetchContent(ctx, urlStr, userAgent, maxContentLength, extractTextOnly)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to fetch content: %v", err),
		}, nil
	}

	result.ProcessingTime = time.Since(start)

	if result.Success && maxOutputChars > 0 && len(result.Content) > maxOutputChars {
		result.Content = result.Content[:maxOutputChars] + "\n\n[Content truncated]"
		result.ContentLength = int64(len(result.Content))
	}

	// Process content with AI prompt if provided
	if result.Success && prompt != "" {
		// For now, just set ProcessedContent to indicate AI processing was requested
		// In a real implementation, this would call an LLM service
		result.ProcessedContent = fmt.Sprintf("AI processing requested with prompt: %s\n\nContent: %s", prompt, result.Content)
	}

	// Prepare metadata
	metadata := map[string]interface{}{
		"url":               urlStr,
		"final_url":         result.FinalURL,
		"status_code":       result.StatusCode,
		"content_type":      result.ContentType,
		"content_length":    result.ContentLength,
		"follow_redirects":  followRedirects,
		"extract_text_only": extractTextOnly,
		"processing_time":   result.ProcessingTime,
	}

	// Format content for display
	userContent := t.formatForUser(result, metadata)
	llmContent := t.formatForLLM(result, metadata)

	return &interfaces.ToolResult{
		Success:     result.Success,
		Data:        result,
		Metadata:    metadata,
		LLMContent:  llmContent,
		UserContent: userContent,
	}, nil
}

func (t *WebFetchTool) fetchContent(ctx context.Context, urlStr, userAgent string, maxContentLength int64, extractTextOnly bool) (*WebFetchResult, error) {
	result := &WebFetchResult{
		URL:           urlStr,
		Headers:       make(map[string]string),
		RedirectChain: []string{},
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to create request: %v", err)
		return result, nil
	}

	// Set headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")

	// Perform request
	resp, err := t.client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("HTTP request failed: %v", err)
		return result, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	// Populate result metadata
	result.StatusCode = resp.StatusCode
	result.FinalURL = resp.Request.URL.String()
	result.ContentType = resp.Header.Get("Content-Type")

	// Copy response headers
	for key, values := range resp.Header {
		if len(values) > 0 {
			result.Headers[key] = values[0]
		}
	}

	// Check if response is successful
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("HTTP error: %d %s", resp.StatusCode, resp.Status)
		return result, nil
	}

	// Read content with size limit
	var reader io.Reader = resp.Body
	if maxContentLength > 0 {
		reader = io.LimitReader(resp.Body, maxContentLength)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to read response body: %v", err)
		return result, nil
	}

	result.ContentLength = int64(len(content))

	// Detect if content is binary before string conversion
	detection := DetectBinaryContent(content)
	if detection.IsBinary {
		result.Error = fmt.Sprintf("Cannot process binary content (detected as %s with %.2f confidence). Content-Type: %s",
			detection.Encoding, detection.Confidence, result.ContentType)
		return result, nil
	}

	contentStr := string(content)

	// Process content based on type
	if extractTextOnly || strings.Contains(result.ContentType, "text/html") {
		result.Content = t.htmlToMarkdown(contentStr)
	} else {
		result.Content = contentStr
	}

	result.Success = true
	return result, nil
}

func (t *WebFetchTool) htmlToMarkdown(html string) string {
	// Simple HTML to text conversion
	// Remove script and style tags and their content
	scriptRegex := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	html = scriptRegex.ReplaceAllString(html, "")

	styleRegex := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	html = styleRegex.ReplaceAllString(html, "")

	// Remove HTML comments
	commentRegex := regexp.MustCompile(`<!--.*?-->`)
	html = commentRegex.ReplaceAllString(html, "")

	// Convert some HTML elements to markdown-like format
	html = regexp.MustCompile(`(?i)<h([1-6])[^>]*>(.*?)</h[1-6]>`).ReplaceAllStringFunc(html, func(match string) string {
		submatches := regexp.MustCompile(`(?i)<h([1-6])[^>]*>(.*?)</h[1-6]>`).FindStringSubmatch(match)
		if len(submatches) >= 3 {
			level := submatches[1]
			content := t.stripHtmlTags(submatches[2])
			switch level {
			case "1":
				return "\n# " + content + "\n"
			case "2":
				return "\n## " + content + "\n"
			case "3":
				return "\n### " + content + "\n"
			default:
				return "\n#### " + content + "\n"
			}
		}
		return match
	})

	// Convert paragraphs
	html = regexp.MustCompile(`(?i)<p[^>]*>(.*?)</p>`).ReplaceAllStringFunc(html, func(match string) string {
		content := regexp.MustCompile(`(?i)<p[^>]*>(.*?)</p>`).FindStringSubmatch(match)
		if len(content) >= 2 {
			return "\n" + t.stripHtmlTags(content[1]) + "\n"
		}
		return match
	})

	// Convert line breaks
	html = regexp.MustCompile(`(?i)<br[^>]*/?>`).ReplaceAllString(html, "\n")

	// Convert links
	html = regexp.MustCompile(`(?i)<a[^>]*href=["']([^"']*)["'][^>]*>(.*?)</a>`).ReplaceAllStringFunc(html, func(match string) string {
		submatches := regexp.MustCompile(`(?i)<a[^>]*href=["']([^"']*)["'][^>]*>(.*?)</a>`).FindStringSubmatch(match)
		if len(submatches) >= 3 {
			url := submatches[1]
			text := t.stripHtmlTags(submatches[2])
			return fmt.Sprintf("[%s](%s)", text, url)
		}
		return match
	})

	// Convert lists
	html = regexp.MustCompile(`(?i)<li[^>]*>(.*?)</li>`).ReplaceAllStringFunc(html, func(match string) string {
		content := regexp.MustCompile(`(?i)<li[^>]*>(.*?)</li>`).FindStringSubmatch(match)
		if len(content) >= 2 {
			return "- " + t.stripHtmlTags(content[1]) + "\n"
		}
		return match
	})

	// Remove remaining HTML tags only (not decoded entities)
	// Use a regex that matches common HTML tags but preserves content like <test>
	commonTags := []string{"html", "head", "body", "title", "div", "span", "p", "a", "img", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "table", "tr", "td", "th", "thead", "tbody", "strong", "em", "b", "i", "u", "s", "sub", "sup", "code", "pre", "blockquote", "section", "article", "header", "footer", "nav", "aside", "main", "figure", "figcaption", "details", "summary", "mark", "small", "del", "ins", "q", "cite", "abbr", "dfn", "time", "var", "samp", "kbd", "ruby", "rt", "rp", "bdi", "bdo", "wbr", "area", "base", "col", "embed", "input", "link", "meta", "param", "source", "track", "audio", "video", "canvas", "svg", "math", "script", "style", "noscript", "template", "slot"}
	for _, tag := range commonTags {
		// Remove opening tags
		tagRegex := regexp.MustCompile(`(?i)<` + tag + `[^>]*>`)
		html = tagRegex.ReplaceAllString(html, "")
		// Remove closing tags
		closeTagRegex := regexp.MustCompile(`(?i)</` + tag + `>`)
		html = closeTagRegex.ReplaceAllString(html, "")
	}

	// Clean up whitespace
	html = regexp.MustCompile(`\n\s*\n\s*\n`).ReplaceAllString(html, "\n\n")
	html = strings.TrimSpace(html)

	return html
}

func (t *WebFetchTool) stripHtmlTags(content string) string { //nolint:revive
	// Remove all HTML tags
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	content = tagRegex.ReplaceAllString(content, "")

	// Decode common HTML entities
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&quot;", "\"")
	content = strings.ReplaceAll(content, "&#39;", "'")
	content = strings.ReplaceAll(content, "&nbsp;", " ")

	return strings.TrimSpace(content)
}

func (t *WebFetchTool) formatForUser(result *WebFetchResult, metadata map[string]interface{}) string { //nolint:revive
	var output strings.Builder

	output.WriteString("🌐 Web Fetch Results\n")
	output.WriteString("─────────────────────────────────────\n")
	fmt.Fprintf(&output, "🔗 URL: %s\n", result.URL)

	if result.FinalURL != result.URL {
		fmt.Fprintf(&output, "📍 Final URL: %s\n", result.FinalURL)
	}

	if result.Success {
		fmt.Fprintf(&output, "✅ Status: %d\n", result.StatusCode)
		fmt.Fprintf(&output, "📄 Content Type: %s\n", result.ContentType)
		fmt.Fprintf(&output, "📏 Content Length: %d bytes\n", result.ContentLength)
		fmt.Fprintf(&output, "⏱️  Processing Time: %v\n", result.ProcessingTime)

		output.WriteString("─────────────────────────────────────\n")
		output.WriteString("📝 Content:\n")

		content := result.Content
		if result.ProcessedContent != "" {
			content = result.ProcessedContent
		}

		// Limit display length
		if len(content) > 2000 {
			output.WriteString(content[:2000])
			output.WriteString("\n\n... (content truncated)")
		} else {
			output.WriteString(content)
		}
	} else {
		fmt.Fprintf(&output, "❌ Error: %s\n", result.Error)
		if result.StatusCode > 0 {
			fmt.Fprintf(&output, "📊 Status Code: %d\n", result.StatusCode)
		}
	}

	return output.String()
}

func (t *WebFetchTool) formatForLLM(result *WebFetchResult, metadata map[string]interface{}) string { //nolint:revive
	var output strings.Builder

	fmt.Fprintf(&output, "Web fetch from: %s\n", result.URL)

	if result.Success {
		fmt.Fprintf(&output, "Status: %d\n", result.StatusCode)
		fmt.Fprintf(&output, "Content-Type: %s\n", result.ContentType)
		fmt.Fprintf(&output, "Content-Length: %d bytes\n", result.ContentLength)

		if result.FinalURL != result.URL {
			fmt.Fprintf(&output, "Final URL: %s\n", result.FinalURL)
		}

		output.WriteString("\nContent:\n")

		content := result.Content
		if result.ProcessedContent != "" {
			content = result.ProcessedContent
		}

		output.WriteString(content)
	} else {
		fmt.Fprintf(&output, "Error: %s\n", result.Error)
		if result.StatusCode > 0 {
			fmt.Fprintf(&output, "Status Code: %d\n", result.StatusCode)
		}
	}

	return output.String()
}
