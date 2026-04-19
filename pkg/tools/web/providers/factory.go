package providers //nolint:revive

import (
	"fmt"
	"sync"
)

// ProviderFactory manages the creation and lifecycle of image providers
type ProviderFactory struct {
	providers map[string]ImageProvider
	configs   map[string]*ProviderConfig
	mu        sync.RWMutex
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{
		providers: make(map[string]ImageProvider),
		configs:   make(map[string]*ProviderConfig),
	}
}

// RegisterProvider registers a provider with the factory
func (f *ProviderFactory) RegisterProvider(name string, provider ImageProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.providers[name] = provider
}

// GetProvider returns a provider by name
func (f *ProviderFactory) GetProvider(name string) (ImageProvider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	provider, exists := f.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider '%s' not found", name)
	}

	return provider, nil
}

// GetAvailableProviders returns a list of available providers
func (f *ProviderFactory) GetAvailableProviders() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var available []string
	for name, provider := range f.providers {
		if provider.IsAvailable() {
			available = append(available, name)
		}
	}

	return available
}

// GetProviderInfo returns information about all registered providers
func (f *ProviderFactory) GetProviderInfo() []ProviderInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var info []ProviderInfo
	for name, provider := range f.providers {
		info = append(info, ProviderInfo{
			Name:         name,
			Description:  provider.Description(),
			Capabilities: f.getProviderCapabilities(name),
			Models:       provider.GetSupportedModels(),
		})
	}

	return info
}

// getProviderCapabilities returns capabilities for a specific provider
func (f *ProviderFactory) getProviderCapabilities(name string) ProviderCapabilities {
	switch name {
	case "openrouter":
		return ProviderCapabilities{
			TextToImage:     true,
			ImageToImage:    true,
			BatchGeneration: false,
			AspectRatio:     true,
			SizeControl:     true,
			StyleControl:    false,
		}
	case "seedream":
		return ProviderCapabilities{
			TextToImage:     true,
			ImageToImage:    true,
			BatchGeneration: true,
			AspectRatio:     true,
			SizeControl:     true,
			StyleControl:    true,
		}
	default:
		return ProviderCapabilities{
			TextToImage: true,
		}
	}
}

// CreateProvider creates a provider instance based on configuration
func (f *ProviderFactory) CreateProvider(name string, config *ProviderConfig) (ImageProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("provider configuration cannot be nil")
	}

	switch name {
	case "openrouter":
		return NewOpenRouterProvider(config), nil
	case "seedream":
		return NewSeedreamProvider(config), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}

// InitializeProviders initializes providers from configuration
func (f *ProviderFactory) InitializeProviders(configs map[string]*ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for name, cfg := range configs {
		if cfg == nil || !cfg.Enabled {
			continue
		}

		provider, err := f.CreateProvider(name, cfg)
		if err != nil {
			continue
		}

		f.providers[name] = provider
		f.configs[name] = cfg
	}

	if len(f.providers) == 0 {
		return fmt.Errorf("no providers were successfully initialized")
	}

	return nil
}

// GetDefaultProvider returns the default provider (first available one)
func (f *ProviderFactory) GetDefaultProvider() (ImageProvider, error) {
	available := f.GetAvailableProviders()
	if len(available) == 0 {
		return nil, fmt.Errorf("no available providers")
	}

	return f.GetProvider(available[0])
}
