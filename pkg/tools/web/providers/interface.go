package providers

import (
	"context"
	"time"
)

// ImageGenerationRequest represents a request to generate an image
type ImageGenerationRequest struct {
	Prompt      string                 `json:"prompt"`
	AspectRatio string                 `json:"aspect_ratio,omitempty"`
	InputImages []string               `json:"input_images,omitempty"` // URLs or base64 data
	Size        string                 `json:"size,omitempty"`
	Model       string                 `json:"model,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ImageGenerationResult represents the result of image generation
type ImageGenerationResult struct {
	URLs        []string `json:"urls"`
	Prompt      string   `json:"prompt"`
	AspectRatio string   `json:"aspect_ratio,omitempty"`
	InputImages []string `json:"input_images,omitempty"`
	IsEdit      bool     `json:"is_edit"`
	Success     bool     `json:"success"`
	Error       string   `json:"error,omitempty"`
}

// ImageProvider defines the interface for image generation providers
type ImageProvider interface {
	// Name returns the provider name (e.g., "openrouter", "seedream")
	Name() string

	// Description returns a brief description of the provider
	Description() string

	// IsAvailable checks if the provider is properly configured and available
	IsAvailable() bool

	// GenerateImages generates images based on the request
	GenerateImages(ctx context.Context, request *ImageGenerationRequest) (*ImageGenerationResult, error)

	// SupportsModel checks if the provider supports the given model
	SupportsModel(model string) bool

	// GetSupportedModels returns a list of supported models
	GetSupportedModels() []string
}

// ProviderConfig holds common configuration for all providers
type ProviderConfig struct {
	APIKey     string            `json:"api_key" yaml:"api_key"`
	BaseURL    string            `json:"base_url" yaml:"base_url"`
	Model      string            `json:"model" yaml:"model"`
	EndpointID string            `json:"endpoint_id" yaml:"endpoint_id"` // VolcEngine ARK endpoint ID for Seedream
	Timeout    time.Duration     `json:"timeout" yaml:"timeout"`
	Headers    map[string]string `json:"headers" yaml:"headers"`
	MaxRetries int               `json:"max_retries" yaml:"max_retries"`
	RetryDelay time.Duration     `json:"retry_delay" yaml:"retry_delay"`
	Enabled    bool              `json:"enabled" yaml:"enabled"`
}

// ProviderCapabilities describes what a provider can do
type ProviderCapabilities struct {
	TextToImage     bool `json:"text_to_image"`
	ImageToImage    bool `json:"image_to_image"`
	BatchGeneration bool `json:"batch_generation"`
	AspectRatio     bool `json:"aspect_ratio"`
	SizeControl     bool `json:"size_control"`
	StyleControl    bool `json:"style_control"`
}

// ProviderInfo contains metadata about a provider
type ProviderInfo struct {
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	Capabilities ProviderCapabilities `json:"capabilities"`
	Models       []string             `json:"models"`
	Config       *ProviderConfig      `json:"config,omitempty"`
}
