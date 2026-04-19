package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouterProvider implements the ImageProvider interface for OpenRouter
type OpenRouterProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider instance
func NewOpenRouterProvider(cfg *ProviderConfig) *OpenRouterProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 1 * time.Second
	}

	return &OpenRouterProvider{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the provider name
func (p *OpenRouterProvider) Name() string {
	return "openrouter"
}

// Description returns a brief description of the provider
func (p *OpenRouterProvider) Description() string {
	return "OpenRouter image generation provider with access to multiple AI models"
}

// IsAvailable checks if the provider is properly configured and available
func (p *OpenRouterProvider) IsAvailable() bool {
	return p.config != nil && p.config.APIKey != ""
}

// SupportsModel checks if the provider supports the given model
func (p *OpenRouterProvider) SupportsModel(model string) bool {
	supportedModels := p.GetSupportedModels()
	for _, supported := range supportedModels {
		if supported == model {
			return true
		}
	}
	return false
}

// GetSupportedModels returns a list of supported models
func (p *OpenRouterProvider) GetSupportedModels() []string {
	return []string{
		"google/gemini-2.5-flash-image",
	}
}

// GenerateImages generates images based on the request
func (p *OpenRouterProvider) GenerateImages(ctx context.Context, request *ImageGenerationRequest) (*ImageGenerationResult, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("OpenRouter provider is not available (missing API key)")
	}

	// Use configured model or request model
	model := request.Model
	if model == "" {
		model = p.config.Model
	}
	if model == "" {
		model = "google/gemini-2.0-flash-exp:free"
	}

	// Build the request
	reqBody, err := p.buildRequest(request, model)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Make the API call with retries
	var resp *http.Response
	var lastError error
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(p.config.RetryDelay * time.Duration(attempt))
		}

		resp, err = p.makeAPICall(ctx, reqBody)
		if err == nil && resp.StatusCode == http.StatusOK {
			// Success - break out of retry loop
			break
		}

		if err != nil {
			lastError = err
			if attempt < p.config.MaxRetries {
				fmt.Printf("OpenRouter API call failed, retrying attempt %d/%d: %v\n",
					attempt+1, p.config.MaxRetries, err)
			}
		} else {
			// Read error response body for logging
			if resp != nil && resp.Body != nil {
				errorBody, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				lastError = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(errorBody))
				if attempt < p.config.MaxRetries {
					fmt.Printf("OpenRouter API call failed, retrying attempt %d/%d: %s\n",
						attempt+1, p.config.MaxRetries, lastError)
				}
			}
		}
	}

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter API call failed after %d retries: %w", p.config.MaxRetries, lastError)
	}

	// Parse the response
	result, err := p.parseResponse(resp, request)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// buildRequest builds the OpenRouter API request
func (p *OpenRouterProvider) buildRequest(request *ImageGenerationRequest, model string) ([]byte, error) {
	// Build content array with text and optional image
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": request.Prompt,
		},
	}

	// Add input images if provided (for image-to-image). Multiple images are sent
	// as separate content array entries, following OpenRouter/Gemini conventions.
	if len(request.InputImages) > 0 {
		for _, img := range request.InputImages {
			if strings.TrimSpace(img) == "" {
				continue
			}
			imageData, err := p.processInputImage(img)
			if err != nil {
				return nil, fmt.Errorf("failed to process input image: %w", err)
			}

			// For OpenRouter, use image_url format with base64 data
			imageURL := fmt.Sprintf("data:image/jpeg;base64,%s", imageData)
			content = append(content, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": imageURL,
				},
			})
		}
	}

	openRouterReq := OpenRouterRequest{
		Model: model,
		Messages: []Message{
			{
				Role:    "user",
				Content: content,
			},
		},
		// Required for image generation
		Modalities: []string{"image", "text"},
	}

	// Add image_config for aspect ratio if specified
	if request.AspectRatio != "" {
		openRouterReq.ImageConfig = map[string]interface{}{
			"aspect_ratio": request.AspectRatio,
		}
	}

	return json.Marshal(openRouterReq)
}

// makeAPICall makes the actual HTTP request to OpenRouter
func (p *OpenRouterProvider) makeAPICall(ctx context.Context, body []byte) (*http.Response, error) {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("HTTP-Referer", "https://github.com/nano-agent/nano-agent")
	req.Header.Set("X-Title", "Nano Agent")

	// Add custom headers from config
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	return p.client.Do(req)
}

// parseResponse parses the OpenRouter API response
func (p *OpenRouterProvider) parseResponse(resp *http.Response, request *ImageGenerationRequest) (*ImageGenerationResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Printf("OpenRouter response status: %d\n", resp.StatusCode)
	fmt.Printf("OpenRouter response body: %s\n", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var openRouterResp OpenRouterResponse
	if err := json.Unmarshal(body, &openRouterResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(openRouterResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openRouterResp.Choices[0]

	// First, check if the message has images field directly
	var imageURLs []string
	if choice.Message.Images != nil && len(choice.Message.Images) > 0 { //nolint:staticcheck
		// Extract image URLs from the images field
		for _, img := range choice.Message.Images {
			if img.ImageURL != nil && img.ImageURL.URL != "" {
				imageURLs = append(imageURLs, img.ImageURL.URL)
			}
		}
	}

	// If no images found in the images field, try to extract from content
	if len(imageURLs) == 0 {
		// Extract text content from the message
		var content string
		switch v := choice.Message.Content.(type) {
		case string:
			content = v
		case []interface{}:
			// If it's an array, find the text content
			for _, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemMap["type"] == "text" {
						if text, ok := itemMap["text"].(string); ok {
							content = text
							break
						}
					}
				}
			}
		default:
			return nil, fmt.Errorf("unexpected content type: %T", v)
		}

		// Extract image URLs from the response content
		imageURLs = p.extractImageURLs(content)
		if len(imageURLs) == 0 {
			// If no URLs found, check if content contains base64 image data
			if strings.Contains(content, "data:image") {
				imageURLs = append(imageURLs, content)
			}
		}
	}

	if len(imageURLs) == 0 {
		return nil, fmt.Errorf("no image URLs found in response")
	}

	return &ImageGenerationResult{
		URLs:        imageURLs,
		Prompt:      request.Prompt,
		AspectRatio: request.AspectRatio,
		InputImages: request.InputImages,
		IsEdit:      len(request.InputImages) > 0,
		Success:     true,
	}, nil
}

// extractImageURLs extracts image URLs from the response content
func (p *OpenRouterProvider) extractImageURLs(content string) []string {
	var urls []string
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http") && (strings.HasSuffix(line, ".jpg") ||
			strings.HasSuffix(line, ".jpeg") ||
			strings.HasSuffix(line, ".png") ||
			strings.HasSuffix(line, ".webp")) {
			urls = append(urls, line)
		}
	}

	return urls
}

// processInputImage processes the input image for image-to-image generation
func (p *OpenRouterProvider) processInputImage(imageInput string) (string, error) {
	// If it's already base64, return as is
	if strings.HasPrefix(imageInput, "data:image") {
		parts := strings.Split(imageInput, ",")
		if len(parts) == 2 {
			return parts[1], nil
		}
	}

	// If it's a URL, download and convert to base64
	if strings.HasPrefix(imageInput, "http") {
		return p.downloadImageToBase64(imageInput)
	}

	// Assume it's already base64 data
	return imageInput, nil
}

// downloadImageToBase64 downloads an image from URL and converts to base64
func (p *OpenRouterProvider) downloadImageToBase64(imageURL string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image data: %w", err)
	}

	base64Data := base64.StdEncoding.EncodeToString(imageData)
	return base64Data, nil
}

// OpenRouterRequest represents the request structure for OpenRouter API
type OpenRouterRequest struct {
	Model       string                 `json:"model"`
	Messages    []Message              `json:"messages"`
	Modalities  []string               `json:"modalities,omitempty"`
	ImageConfig map[string]interface{} `json:"image_config,omitempty"`
}

// Message represents a message in the OpenRouter API
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or array
	Images  []ImageData `json:"images,omitempty"`
}

// ImageData represents image data in the response
type ImageData struct {
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in the response
type ImageURL struct {
	URL string `json:"url"`
}

// OpenRouterResponse represents the response structure from OpenRouter API
type OpenRouterResponse struct {
	Choices []Choice `json:"choices"`
}

// Choice represents a choice in the OpenRouter response
type Choice struct {
	Message Message `json:"message"`
}
