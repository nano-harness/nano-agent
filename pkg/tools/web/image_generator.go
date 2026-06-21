package web //nolint:revive

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/web/providers"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// ImageGenerateTool integrates image generation models via multiple providers
type ImageGenerateTool struct {
	providerFactory *providers.ProviderFactory
	ossClient       *oss.Client
}

// NewImageGenerateTool creates a new ImageGenerateTool instance
func NewImageGenerateTool() *ImageGenerateTool {
	// Initialize provider factory
	factory := providers.NewProviderFactory()

	// Get global configuration to check which providers are configured
	globalCfg := config.Get()

	// Register providers that have valid configuration (API key present and enabled)
	if globalCfg != nil && globalCfg.ImageGenerator != nil {
		enabledProviders := globalCfg.ImageGenerator.GetEnabledProviders()
		for _, providerConfig := range enabledProviders {
			// Skip providers that are explicitly disabled or have no API key
			if !providerConfig.Enabled || providerConfig.APIKey == "" {
				continue
			}

			// Create provider config
			config := &providers.ProviderConfig{
				APIKey:     providerConfig.APIKey,
				BaseURL:    providerConfig.BaseURL,
				Model:      providerConfig.Model,
				EndpointID: providerConfig.EndpointID,
				Enabled:    providerConfig.Enabled,
				MaxRetries: 3,
				RetryDelay: time.Second,
				Timeout:    10 * time.Minute,
			}

			switch providerConfig.Provider {
			case "openrouter":
				factory.RegisterProvider("openrouter", providers.NewOpenRouterProvider(config))
			case "seedream":
				factory.RegisterProvider("seedream", providers.NewSeedreamProvider(config))
				// Add more providers here as they are implemented
			}
		}
	}

	// Initialize OSS client if enabled
	var ossClient *oss.Client
	if globalCfg != nil && globalCfg.OSS != nil && globalCfg.OSS.Enabled {
		var err error
		ossClient, err = oss.New(globalCfg.OSS.NormalizedEndpoint(), globalCfg.OSS.AccessKeyID, globalCfg.OSS.AccessKeySecret)
		if err != nil {
			// Log the error but don't block tool initialization
			logger.Warnf("Failed to initialize OSS client: %v", err)
		}
	}

	return &ImageGenerateTool{
		providerFactory: factory,
		ossClient:       ossClient,
	}
}

func (t *ImageGenerateTool) Name() string { return "image_generate" } //nolint:revive

func (t *ImageGenerateTool) Description() string { //nolint:revive
	return "Generate or edit images via multiple providers (OpenRouter, Seedream)"
}

func (t *ImageGenerateTool) Category() interfaces.ToolCategory { return interfaces.CategoryWeb } //nolint:revive

func (t *ImageGenerateTool) RequiresConfirmation() bool { return true } //nolint:revive

// ConcurrencySafe returns false: image generation may write files to disk.
func (t *ImageGenerateTool) ConcurrencySafe() bool { return false }

func (t *ImageGenerateTool) Schema() *interfaces.ToolSchema { //nolint:revive
	promptProp := interfaces.NewStringProperty("Text prompt to describe the image to generate or edit")
	promptProp.Examples = []string{"A nano banana dish in a fancy restaurant", "夕阳下的未来城市", "Change the background to a sunset scene"}
	promptProp.Usage = "Use clear, visual language; include style cues. For editing, describe what changes you want to make."

	aspectProp := interfaces.NewStringPropertyWithEnum("Image aspect ratio", []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"})
	aspectProp.Examples = []string{"16:9", "1:1"}
	aspectProp.Usage = "Optional; defaults to 1:1."

	imageUrlsProp := interfaces.NewArrayProperty("输入图片URL数组（支持多张，编辑模式可用）", "string")
	imageUrlsProp.Examples = []string{"https://example.com/image1.jpg", "https://example.com/image2.png", "data:image/png;base64,...."}
	imageUrlsProp.Usage = "支持一张或多张输入图片，数组形式。留空为纯生成模式。支持HTTP/HTTPS与base64 data URL。"

	providerProp := interfaces.NewStringPropertyWithEnum("图片生成提供商", []string{"openrouter", "seedream"})
	providerProp.Usage = "Optional; defaults to the configured default provider."

	return interfaces.CreateSchema(
		"Generate or edit images using OpenRouter API",
		map[string]*interfaces.PropertySchema{
			"prompt":       promptProp,
			"aspect_ratio": aspectProp,
			"image_urls":   imageUrlsProp,
			"provider":     providerProp,
		},
		[]string{"prompt"},
	)
}

// validateParams validates the input parameters
func (t *ImageGenerateTool) validateParams(params map[string]interface{}) error {
	if params == nil {
		return fmt.Errorf("tool parameters are missing")
	}

	// Validate prompt
	prompt, ok := params["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required and must be a non-empty string")
	}

	// Validate aspect_ratio if provided
	if aspect, exists := params["aspect_ratio"]; exists {
		if aspectStr, ok := aspect.(string); ok {
			validAspects := []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}
			valid := false
			for _, validAspect := range validAspects {
				if aspectStr == validAspect {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid aspect_ratio: %s. Valid options: %v", aspectStr, validAspects)
			}
		}
	}

	// No legacy `image_url` support; ignore if present.

	// Validate image_urls if provided
	if imageURLs, exists := params["image_urls"]; exists {
		switch v := imageURLs.(type) {
		case []string:
			if len(v) == 0 {
				return fmt.Errorf("image_urls cannot be empty")
			}
			for _, s := range v {
				s = strings.TrimSpace(s)
				if s == "" {
					return fmt.Errorf("image_urls contains empty string")
				}
				if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "data:image/") {
					return fmt.Errorf("image_urls must contain valid HTTP/HTTPS URLs or base64 data URLs")
				}
			}
		case []interface{}:
			if len(v) == 0 {
				return fmt.Errorf("image_urls cannot be empty")
			}
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return fmt.Errorf("image_urls must be an array of strings")
				}
				s = strings.TrimSpace(s)
				if s == "" {
					return fmt.Errorf("image_urls contains empty string")
				}
				if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "data:image/") {
					return fmt.Errorf("image_urls must contain valid HTTP/HTTPS URLs or base64 data URLs")
				}
			}
		default:
			return fmt.Errorf("image_urls must be an array of strings")
		}
	}

	// If both image_url and image_urls are provided, we'll merge them later in Execute.

	return nil
}

// getProviderAndConfig retrieves the appropriate provider and its configuration
func (t *ImageGenerateTool) getProviderAndConfig(providerName string) (providers.ImageProvider, *providers.ProviderConfig, error) {
	globalCfg := config.Get()

	if globalCfg.ImageGenerator == nil {
		return nil, nil, fmt.Errorf("image generator configuration is missing")
	}

	// Get provider config
	var providerConfig *config.ImageGeneratorProviderConfig
	if providerName != "" {
		var found bool
		providerConfig, found = globalCfg.ImageGenerator.GetProvider(providerName)
		if !found || providerConfig == nil {
			return nil, nil, fmt.Errorf("provider %s not found", providerName)
		}
	} else {
		var found bool
		providerConfig, found = globalCfg.ImageGenerator.GetDefaultProvider()
		if !found || providerConfig == nil {
			return nil, nil, fmt.Errorf("no configured provider found")
		}
	}

	// Get provider from factory
	provider, err := t.providerFactory.GetProvider(providerConfig.Provider)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get provider %s: %v", providerConfig.Provider, err)
	}

	// Create provider config
	config := &providers.ProviderConfig{
		APIKey:     providerConfig.APIKey,
		BaseURL:    providerConfig.BaseURL,
		Model:      providerConfig.Model,
		EndpointID: providerConfig.EndpointID,
		Enabled:    providerConfig.Enabled,
		MaxRetries: 3,
		RetryDelay: time.Second,
	}

	return provider, config, nil
}

// uploadImageToOSS uploads image data to OSS and returns a presigned URL with 7-day expiration
func (t *ImageGenerateTool) uploadImageToOSS(ctx context.Context, imageData []byte, fileName string) (string, error) { //nolint:revive
	if t.ossClient == nil {
		return "", fmt.Errorf("OSS client not initialized")
	}

	globalCfg := config.Get()
	if globalCfg.OSS == nil || !globalCfg.OSS.Enabled {
		return "", fmt.Errorf("OSS is not enabled in configuration")
	}

	bucketName := globalCfg.OSS.DefaultBucket
	if bucketName == "" {
		return "", fmt.Errorf("OSS default bucket not configured")
	}

	bucket, err := t.ossClient.Bucket(bucketName)
	if err != nil {
		return "", fmt.Errorf("failed to get OSS bucket %s: %w", bucketName, err)
	}

	// Use a unique object name to avoid conflicts
	objectName := fmt.Sprintf("images/%s-%d.png", strings.TrimSuffix(fileName, ".png"), time.Now().UnixNano())

	err = bucket.PutObject(objectName, bytes.NewReader(imageData))
	if err != nil {
		return "", fmt.Errorf("failed to upload image to OSS: %w", err)
	}

	// Generate a presigned URL with 7-day expiration (7 * 24 * 60 * 60 = 604800 seconds)
	presignedURL, err := bucket.SignURL(objectName, oss.HTTPGet, 604800)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedURL, nil
}

// downloadImageFromURL downloads an image from a URL and returns the image data
func (t *ImageGenerateTool) downloadImageFromURL(ctx context.Context, imageURL string) ([]byte, error) { //nolint:unused
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download image, status code: %d", resp.StatusCode)
	}

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	return imageData, nil
}

// decodeBase64Image decodes a base64 data URL and returns the image data
func (t *ImageGenerateTool) decodeBase64Image(dataURL string) ([]byte, error) {
	// Check if it's a data URL (data:image/png;base64,...)
	if !strings.HasPrefix(dataURL, "data:image/") {
		return nil, fmt.Errorf("invalid data URL format")
	}

	// Find the base64 data part
	parts := strings.Split(dataURL, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid data URL format")
	}

	base64Data := parts[1]
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	return imageData, nil
}

// transferImageToOSS transfers an image (URL or base64) to OSS and returns the OSS URL
func (t *ImageGenerateTool) transferImageToOSS(ctx context.Context, imageURL string, index int) (string, error) {
	var fileName string

	if strings.HasPrefix(imageURL, "data:image/") {
		// Handle base64 data URL - still need to decode to memory for base64
		imageData, err := t.decodeBase64Image(imageURL)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 image: %w", err)
		}
		fileName = fmt.Sprintf("generated_image_%d", index)

		// Upload to OSS using traditional method for base64 data
		ossURL, err := t.uploadImageToOSS(ctx, imageData, fileName)
		if err != nil {
			return "", fmt.Errorf("failed to upload image to OSS: %w", err)
		}
		return ossURL, nil
	} else { //nolint:revive
		// Handle regular URL - use streaming upload for better performance
		// Extract filename from URL or use default
		fileName = filepath.Base(imageURL)
		if fileName == "." || fileName == "/" {
			fileName = fmt.Sprintf("downloaded_image_%d", index)
		}
		// Remove extension if present, streamImageToOSS will add .png
		if ext := filepath.Ext(fileName); ext != "" {
			fileName = strings.TrimSuffix(fileName, ext)
		}

		// Stream directly from URL to OSS without loading into memory
		ossURL, err := t.streamImageToOSS(ctx, imageURL, fileName)
		if err != nil {
			return "", fmt.Errorf("failed to stream image to OSS: %w", err)
		}
		return ossURL, nil
	}
}

func (t *ImageGenerateTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	// Validate parameters
	if err := t.validateParams(params); err != nil {
		return &interfaces.ToolResult{Success: false, Error: err.Error()}, nil
	}

	// Extract parameters
	prompt := strings.TrimSpace(params["prompt"].(string))
	aspectRatio := ""
	if aspect, ok := params["aspect_ratio"].(string); ok {
		aspectRatio = strings.TrimSpace(aspect)
	}
	var inputImageURLs []string
	// Multiple image_urls
	if raw, ok := params["image_urls"]; ok {
		switch v := raw.(type) {
		case []string:
			for _, s := range v {
				s = strings.TrimSpace(s)
				if s != "" {
					inputImageURLs = append(inputImageURLs, s)
				}
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						inputImageURLs = append(inputImageURLs, s)
					}
				}
			}
		}
	}

	// Optional provider parameter
	providerName := ""
	if provider, ok := params["provider"].(string); ok {
		providerName = strings.TrimSpace(provider)
	}

	// Get provider and configuration
	provider, providerConfig, err := t.getProviderAndConfig(providerName)
	if err != nil {
		msg := fmt.Sprintf("Configuration error: %v", err)
		return &interfaces.ToolResult{Success: false, Error: err.Error(), LLMContent: msg, UserContent: msg}, nil
	}

	// Prepare request
	request := &providers.ImageGenerationRequest{
		Prompt:      prompt,
		AspectRatio: aspectRatio,
		InputImages: inputImageURLs,
		Model:       providerConfig.Model,
	}

	// Generate images using the provider
	result, err := provider.GenerateImages(ctx, request)
	if err != nil {
		llm := fmt.Sprintf("Image generation error with %s: %v", provider.Name(), err)
		return &interfaces.ToolResult{Success: false, Error: err.Error(), LLMContent: llm, UserContent: llm}, nil
	}

	// Use the URLs from the result
	originalImageURLs := result.URLs

	// Transfer all images to OSS
	var ossImageURLs []string
	var transferErrors []string

	for i, imageURL := range originalImageURLs {
		ossURL, err := t.transferImageToOSS(ctx, imageURL, i+1)
		if err != nil {
			// Log the error but continue with other images
			transferErrors = append(transferErrors, fmt.Sprintf("Image %d: %v", i+1, err))
			// Keep the original URL as fallback
			ossImageURLs = append(ossImageURLs, imageURL)
		} else {
			ossImageURLs = append(ossImageURLs, ossURL)
		}
	}

	// If there were transfer errors, log them but don't fail the entire operation
	if len(transferErrors) > 0 {
		fmt.Printf("Warning: Some images failed to transfer to OSS: %v\n", transferErrors)
	}

	// Create result with OSS URLs
	generationResult := map[string]interface{}{
		"urls":         ossImageURLs,
		"prompt":       prompt,
		"aspect_ratio": aspectRatio,
		"input_images": inputImageURLs,
		"is_edit":      len(inputImageURLs) > 0,
		"success":      true,
		"provider":     provider.Name(),
		"model":        providerConfig.Model,
	}

	// Format final result
	var userMsg, llmMsg string
	if len(inputImageURLs) > 0 {
		// Editing mode
		if len(ossImageURLs) == 1 {
			userMsg = fmt.Sprintf("✅ 已编辑图片并转存到OSS\n提示词: %s\n比例: %s\n提供商: %s\n模型: %s\n![图片](%s)\n(OSS存储，链接7天有效)",
				prompt, aspectRatio, provider.Name(), providerConfig.Model, ossImageURLs[0])
			llmMsg = fmt.Sprintf("Image editing success with %s (%s). Prompt: %s. Aspect: %s. ![image](%s)",
				provider.Name(), providerConfig.Model, prompt, aspectRatio, ossImageURLs[0])
		} else {
			userMsg = fmt.Sprintf("✅ 已编辑生成 %d 张图片并转存到OSS\n提示词: %s\n比例: %s\n提供商: %s\n模型: %s\n",
				len(ossImageURLs), prompt, aspectRatio, provider.Name(), providerConfig.Model)
			llmMsg = fmt.Sprintf("Image editing success with %s (%s). Generated %d images. Prompt: %s. Aspect: %s",
				provider.Name(), providerConfig.Model, len(ossImageURLs), prompt, aspectRatio)
			for i, url := range ossImageURLs {
				userMsg += fmt.Sprintf("图片 %d: ![图片%d](%s)\n", i+1, i+1, url)
				llmMsg += fmt.Sprintf(" Image %d: ![image%d](%s)", i+1, i+1, url)
			}
			userMsg += "(OSS存储，链接7天有效)"
		}
	} else {
		// Generation mode
		if len(ossImageURLs) == 1 {
			userMsg = fmt.Sprintf("✅ 已生成图片并转存到OSS\n提示词: %s\n比例: %s\n提供商: %s\n模型: %s\n![图片](%s)\n(OSS存储，链接7天有效)",
				prompt, aspectRatio, provider.Name(), providerConfig.Model, ossImageURLs[0])
			llmMsg = fmt.Sprintf("Image generation success with %s (%s). ![image](%s)",
				provider.Name(), providerConfig.Model, ossImageURLs[0])
		} else {
			userMsg = fmt.Sprintf("✅ 已生成 %d 张图片并转存到OSS\n提示词: %s\n比例: %s\n提供商: %s\n模型: %s\n",
				len(ossImageURLs), prompt, aspectRatio, provider.Name(), providerConfig.Model)
			llmMsg = fmt.Sprintf("Image generation success with %s (%s). Generated %d images.",
				provider.Name(), providerConfig.Model, len(ossImageURLs))
			for i, url := range ossImageURLs {
				userMsg += fmt.Sprintf("图片 %d: ![图片%d](%s)\n", i+1, i+1, url)
				llmMsg += fmt.Sprintf(" Image %d: ![image%d](%s)", i+1, i+1, url)
			}
			userMsg += "(OSS存储，链接7天有效)"
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        generationResult,
		LLMContent:  llmMsg,
		UserContent: userMsg,
	}, nil
}

// streamImageToOSS streams an image from URL directly to OSS without loading into memory
func (t *ImageGenerateTool) streamImageToOSS(ctx context.Context, imageURL string, fileName string) (string, error) {
	if t.ossClient == nil {
		return "", fmt.Errorf("OSS client not initialized")
	}

	globalCfg := config.Get()
	if globalCfg.OSS == nil || !globalCfg.OSS.Enabled {
		return "", fmt.Errorf("OSS is not enabled in configuration")
	}

	bucketName := globalCfg.OSS.DefaultBucket
	if bucketName == "" {
		return "", fmt.Errorf("OSS default bucket not configured")
	}

	bucket, err := t.ossClient.Bucket(bucketName)
	if err != nil {
		return "", fmt.Errorf("failed to get OSS bucket %s: %w", bucketName, err)
	}

	// Create HTTP request for streaming download with proper headers
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request for URL %s: %w", imageURL, err)
	}

	// Set user agent to avoid being blocked by some services
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ImageGenerator/1.0)")

	client := &http.Client{
		Timeout: 60 * time.Second, // Longer timeout for streaming
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download image from URL %s: %w", imageURL, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log the error but don't override the main error
			fmt.Printf("Warning: failed to close response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image from URL %s, HTTP status: %d %s", imageURL, resp.StatusCode, resp.Status)
	}

	// Validate content type if available
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("invalid content type for image URL %s: %s", imageURL, contentType)
	}

	// Use a unique object name to avoid conflicts
	objectName := fmt.Sprintf("images/%s-%d.png", strings.TrimSuffix(fileName, ".png"), time.Now().UnixNano())

	// Stream directly from HTTP response to OSS
	// Note: OSS SDK doesn't support context directly, but the HTTP client timeout should handle this
	err = bucket.PutObject(objectName, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to stream upload image to OSS (object: %s): %w", objectName, err)
	}

	// Generate a presigned URL with 7-day expiration (7 * 24 * 60 * 60 = 604800 seconds)
	presignedURL, err := bucket.SignURL(objectName, oss.HTTPGet, 604800)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL for object %s: %w", objectName, err)
	}

	return presignedURL, nil
}
