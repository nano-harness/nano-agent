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

// SeedreamProvider implements the ImageProvider interface for ByteDance Seedream
type SeedreamProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewSeedreamProvider creates a new Seedream provider instance
func NewSeedreamProvider(cfg *ProviderConfig) *SeedreamProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second // Seedream may take longer
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second // Longer retry delay for Seedream
	}

	return &SeedreamProvider{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the provider name
func (p *SeedreamProvider) Name() string {
	return "seedream"
}

// Description returns a brief description of the provider
func (p *SeedreamProvider) Description() string {
	return "ByteDance Seedream image generation provider with advanced AI capabilities"
}

// IsAvailable checks if the provider is properly configured and available
func (p *SeedreamProvider) IsAvailable() bool {
	return p.config != nil && p.config.APIKey != ""
}

// SupportsModel checks if the provider supports the given model
func (p *SeedreamProvider) SupportsModel(model string) bool {
	supportedModels := p.GetSupportedModels()
	for _, supported := range supportedModels {
		if supported == model {
			return true
		}
	}
	return false
}

// GetSupportedModels returns a list of supported models
func (p *SeedreamProvider) GetSupportedModels() []string {
	return []string{
		"doubao-seedream-4-0-250828",
	}
}

// GenerateImages generates images based on the request
func (p *SeedreamProvider) GenerateImages(ctx context.Context, request *ImageGenerationRequest) (*ImageGenerationResult, error) {
	if !p.IsAvailable() {
		return nil, fmt.Errorf("Seedream provider is not available (missing API key)") //nolint:staticcheck
	}

	// Use endpoint ID first, then model, then request model
	model := p.config.EndpointID
	if model == "" {
		model = request.Model
	}
	if model == "" {
		model = p.config.Model
	}
	if model == "" {
		model = "seedream"
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
				fmt.Printf("Seedream API call failed, retrying attempt %d/%d: %v\n",
					attempt+1, p.config.MaxRetries, err)
			}
		} else {
			// Read error response body for logging
			if resp != nil && resp.Body != nil {
				errorBody, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				lastError = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(errorBody))
				if attempt < p.config.MaxRetries {
					fmt.Printf("Seedream API call failed, retrying attempt %d/%d: %s\n",
						attempt+1, p.config.MaxRetries, lastError)
				}
			}
		}
	}

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Seedream API call failed after %d retries: %w", p.config.MaxRetries, lastError) //nolint:staticcheck
	}

	// Parse the response
	result, err := p.parseResponse(resp, request)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// buildRequest builds the Seedream API request
func (p *SeedreamProvider) buildRequest(request *ImageGenerationRequest, model string) ([]byte, error) {
	// Build Seedream image generation request according to API documentation
	seedreamRequest := map[string]interface{}{
		"model":  model,
		"prompt": request.Prompt,
	}

	// Add input image(s) if provided (for image-to-image)
	if len(request.InputImages) > 0 {
		if len(request.InputImages) == 1 {
			imageData, err := p.processInputImage(request.InputImages[0])
			if err != nil {
				return nil, fmt.Errorf("failed to process input image: %w", err)
			}
			seedreamRequest["image"] = "data:image/jpeg;base64," + imageData
		} else {
			var images []string
			for _, img := range request.InputImages {
				if strings.TrimSpace(img) == "" {
					continue
				}
				imageData, err := p.processInputImage(img)
				if err != nil {
					return nil, fmt.Errorf("failed to process input image: %w", err)
				}
				images = append(images, "data:image/jpeg;base64,"+imageData)
			}
			if len(images) > 0 {
				seedreamRequest["images"] = images
			}
		}
	}

	// Set size parameter - convert from aspect ratio or use direct size
	if request.Size != "" {
		seedreamRequest["size"] = request.Size
	} else if request.AspectRatio != "" {
		// Convert aspect ratio to size format
		switch request.AspectRatio {
		case "1:1", "square":
			seedreamRequest["size"] = "1K" // 1024x1024
		case "16:9", "landscape":
			seedreamRequest["size"] = "2K" // 2048x1152
		case "9:16", "portrait":
			seedreamRequest["size"] = "2K" // 1152x2048
		default:
			seedreamRequest["size"] = "1K" // default
		}
	} else {
		seedreamRequest["size"] = "1K" // default size
	}

	// Add stream parameter (default to false for non-streaming)
	seedreamRequest["stream"] = false

	// Add any custom options from request
	for k, v := range request.Options {
		seedreamRequest[k] = v
	}

	return json.Marshal(seedreamRequest)
}

// buildOptions builds Seedream-specific options
func (p *SeedreamProvider) buildOptions(request *ImageGenerationRequest) map[string]interface{} { //nolint:unused
	options := make(map[string]interface{})

	// Set aspect ratio
	if request.AspectRatio != "" {
		options["aspect_ratio"] = request.AspectRatio
	} else {
		options["aspect_ratio"] = "1:1" // default
	}

	// Set size if provided
	if request.Size != "" {
		options["size"] = request.Size
	}

	// Add any custom options from request
	for k, v := range request.Options {
		options[k] = v
	}

	// Seedream specific options
	options["quality"] = "high" // default quality
	options["style"] = "art"    // default style

	return options
}

// makeAPICall makes the actual HTTP request to Seedream
func (p *SeedreamProvider) makeAPICall(ctx context.Context, body []byte) (*http.Response, error) {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com" // Default VolcEngine ARK API base URL
	}

	// Use the correct image generation endpoint
	endpoint := baseURL + "/images/generations"

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("X-Date", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("X-Request-ID", generateRequestID())

	// Add custom headers from config
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	fmt.Printf("Making Seedream API call to: %s\n", endpoint)
	fmt.Printf("Request headers: Content-Type=%s, Authorization=Bearer %s...\n",
		req.Header.Get("Content-Type"), p.config.APIKey[:10])

	resp, err := p.client.Do(req)
	if err != nil {
		fmt.Printf("Seedream API call failed: %v\n", err)
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	fmt.Printf("Seedream API response status: %d\n", resp.StatusCode)
	return resp, nil
}

// parseResponse parses the Seedream API response
func (p *SeedreamProvider) parseResponse(resp *http.Response, request *ImageGenerationRequest) (*ImageGenerationResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Printf("Seedream response status: %d\n", resp.StatusCode)
	fmt.Printf("Seedream response body: %s\n", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse as Seedream image generation response
	var seedreamResp SeedreamResponse
	if err := json.Unmarshal(body, &seedreamResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract image URLs from the response
	var imageURLs []string
	if len(seedreamResp.Data) == 0 {
		return nil, fmt.Errorf("no images found in response")
	}

	for _, img := range seedreamResp.Data {
		if img.URL != "" {
			imageURLs = append(imageURLs, img.URL)
		}
	}

	if len(imageURLs) == 0 {
		return nil, fmt.Errorf("no valid image URLs or base64 data found in response")
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

// processInputImage processes the input image for image-to-image generation
func (p *SeedreamProvider) processInputImage(imageInput string) (string, error) {
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
func (p *SeedreamProvider) downloadImageToBase64(imageURL string) (string, error) {
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

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("seedream-%d-%d", time.Now().Unix(), time.Now().Nanosecond())
}

// SeedreamRequest represents the request structure for Seedream API
type SeedreamRequest struct {
	RequestID   string                 `json:"request_id"`
	Prompt      string                 `json:"prompt"`
	Model       string                 `json:"model"`
	InputImages []string               `json:"input_images,omitempty"`
	Options     map[string]interface{} `json:"options"`
}

// SeedreamResponse represents the response structure from Seedream API
type SeedreamResponse struct {
	Model   string          `json:"model"`
	Created int64           `json:"created"`
	Data    []SeedreamImage `json:"data"`
	Usage   SeedreamUsage   `json:"usage"`
}

// SeedreamImage represents a single generated image
type SeedreamImage struct {
	URL  string `json:"url,omitempty"`
	Size string `json:"size,omitempty"`
}

// SeedreamUsage contains usage information
type SeedreamUsage struct {
	GeneratedImages int `json:"generated_images"`
	OutputTokens    int `json:"output_tokens"`
	TotalTokens     int `json:"total_tokens"`
}
