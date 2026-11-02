package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageGeneratorIntegration(t *testing.T) {
	// Skip network dependent tests
	t.Skip("Skipping network dependent tests in TestImageGeneratorIntegration")

	// 加载测试配置 - 尝试多个可能的路径
	configPaths := []string{
		"../../../deployment/daemon-config.yaml",
		"deployment/daemon-config.yaml",
		"./deployment/daemon-config.yaml",
	}

	var err error
	var configLoaded bool

	for _, path := range configPaths {
		_, err = config.LoadConfig(path)
		if err == nil {
			configLoaded = true
			t.Logf("Successfully loaded config from: %s", path)
			break
		}
		t.Logf("Failed to load config from %s: %v", path, err)
	}

	if !configLoaded {
		t.Skip("Could not load configuration file, skipping integration test")
		return
	}

	// 创建图像生成工具
	tool := NewImageGenerateTool()
	require.NotNil(t, tool, "ImageGenerateTool should not be nil")

	ctx := context.Background()

	t.Run("TestOpenRouterProvider", func(t *testing.T) {
		testOpenRouterProvider(t, ctx, tool)
	})

	t.Run("TestSeedreamProvider", func(t *testing.T) {
		testSeedreamProvider(t, ctx, tool)
	})

	t.Run("TestErrorHandling", func(t *testing.T) {
		testErrorHandling(t, ctx, tool)
	})

	t.Run("TestMultiImageValidation", func(t *testing.T) {
		testMultiImageValidation(t, tool)
	})

	t.Run("TestExecuteWithMultiImages", func(t *testing.T) {
		testExecuteWithMultiImages(t, ctx, tool)
	})
}

func testOpenRouterProvider(t *testing.T, ctx context.Context, tool *ImageGenerateTool) {
	params := map[string]interface{}{
		"prompt":       "A beautiful sunset over mountains",
		"aspect_ratio": "16:9",
		"provider":     "openrouter",
	}

	t.Logf("Testing OpenRouter with params: %+v", params)

	// 设置超时
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Logf("OpenRouter execution error: %v", err)
		// 如果是API密钥或网络问题，跳过测试而不是失败
		if isAPIKeyOrNetworkError(err) {
			t.Skip("Skipping OpenRouter test due to API key or network issue")
			return
		}
		t.Fatalf("Unexpected error: %v", err)
	}

	assert.NotNil(t, result, "Result should not be nil")

	if result.Success {
		t.Logf("✅ OpenRouter test successful!")
		t.Logf("LLM Content: %s", result.LLMContent)
		t.Logf("User Content: %s", result.UserContent)

		// 验证结果包含图像URL或数据
		assert.NotEmpty(t, result.LLMContent, "LLM content should not be empty")
		assert.NotEmpty(t, result.UserContent, "User content should not be empty")
	} else {
		t.Logf("❌ OpenRouter test failed: %s", result.Error)
		// 如果是已知的API问题，跳过而不是失败
		if isKnownAPIIssue(result.Error) {
			t.Skip("Skipping OpenRouter test due to known API issue")
			return
		}
		t.Fatalf("OpenRouter test failed: %s", result.Error)
	}
}

func testSeedreamProvider(t *testing.T, ctx context.Context, tool *ImageGenerateTool) {
	params := map[string]interface{}{
		"prompt":       "一只可爱的小猫在花园里玩耍",
		"aspect_ratio": "1:1",
		"provider":     "seedream",
	}

	t.Logf("Testing Seedream with params: %+v", params)

	// 设置超时
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Logf("Seedream execution error: %v", err)
		// 如果是API密钥或网络问题，跳过测试而不是失败
		if isAPIKeyOrNetworkError(err) {
			t.Skip("Skipping Seedream test due to API key or network issue")
			return
		}
		t.Fatalf("Unexpected error: %v", err)
	}

	assert.NotNil(t, result, "Result should not be nil")

	if result.Success {
		t.Logf("✅ Seedream test successful!")
		t.Logf("LLM Content: %s", result.LLMContent)
		t.Logf("User Content: %s", result.UserContent)

		// 验证结果包含图像URL或数据
		assert.NotEmpty(t, result.LLMContent, "LLM content should not be empty")
		assert.NotEmpty(t, result.UserContent, "User content should not be empty")
	} else {
		t.Logf("❌ Seedream test failed: %s", result.Error)
		// 如果是已知的API问题，跳过而不是失败
		if isKnownAPIIssue(result.Error) {
			t.Skip("Skipping Seedream test due to known API issue")
			return
		}
		t.Fatalf("Seedream test failed: %s", result.Error)
	}
}

func testErrorHandling(t *testing.T, ctx context.Context, tool *ImageGenerateTool) {
	t.Run("InvalidProvider", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt":   "test prompt",
			"provider": "invalid_provider",
		}

		result, err := tool.Execute(ctx, params)

		// 应该返回错误或失败结果
		if err == nil {
			assert.False(t, result.Success, "Should fail with invalid provider")
			assert.NotEmpty(t, result.Error, "Should have error message")
		} else {
			t.Logf("Expected error with invalid provider: %v", err)
		}
	})

	t.Run("EmptyPrompt", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt":   "",
			"provider": "openrouter",
		}

		result, err := tool.Execute(ctx, params)

		// 应该返回错误或失败结果
		if err == nil {
			assert.False(t, result.Success, "Should fail with empty prompt")
			assert.NotEmpty(t, result.Error, "Should have error message")
		} else {
			t.Logf("Expected error with empty prompt: %v", err)
		}
	})

	t.Run("InvalidAspectRatio", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt":       "test prompt",
			"aspect_ratio": "invalid:ratio",
			"provider":     "openrouter",
		}

		result, _ := tool.Execute(ctx, params)

		// 可能会失败或使用默认值，但不应该崩溃
		assert.NotNil(t, result, "Result should not be nil even with invalid aspect ratio")
	})

	t.Run("BothImageUrlAndImageUrlsProvided", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt":     "test prompt",
			"provider":   "openrouter",
			"image_url":  "https://example.com/one.png",
			"image_urls": []interface{}{"https://example.com/two.png"},
		}

		// 参数校验不应报错，执行时会合并两者
		err := tool.validateParams(params)
		assert.NoError(t, err)
	})
}

// 验证多图参数校验逻辑
func testMultiImageValidation(t *testing.T, tool *ImageGenerateTool) {
	t.Run("ValidImageUrlsArray", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt": "a test prompt",
			"image_urls": []interface{}{
				"https://example.com/1.png",
				"data:image/png;base64,iVBORw0KGgoAAAANSUhEUg...",
			},
		}
		err := tool.validateParams(params)
		assert.NoError(t, err, "image_urls should be accepted when valid")
	})

	t.Run("EmptyImageUrlsArray", func(t *testing.T) {
		params := map[string]interface{}{
			"prompt":     "a test prompt",
			"image_urls": []interface{}{},
		}
		err := tool.validateParams(params)
		assert.Error(t, err, "empty image_urls should be rejected")
	})
}

// 使用多张输入图片执行（网络或API问题将跳过）
func testExecuteWithMultiImages(t *testing.T, ctx context.Context, tool *ImageGenerateTool) {
	params := map[string]interface{}{
		"prompt":       "Combine and stylize two images into a collage",
		"aspect_ratio": "1:1",
		"provider":     "openrouter",
		"image_urls":   []interface{}{"https://example.com/a.png", "https://example.com/b.jpg"},
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, params)
	if err != nil {
		if isAPIKeyOrNetworkError(err) {
			t.Skip("Skipping multi-image execute due to API key or network issue")
			return
		}
		t.Fatalf("Unexpected error: %v", err)
	}

	assert.NotNil(t, result, "Result should not be nil")
	if result.Success {
		assert.NotEmpty(t, result.LLMContent, "LLM content should not be empty")
		assert.NotEmpty(t, result.UserContent, "User content should not be empty")
	} else {
		if isKnownAPIIssue(result.Error) {
			t.Skip("Skipping multi-image execute due to known API issue")
			return
		}
		t.Logf("Multi-image execute failed: %s", result.Error)
	}
}

// 辅助函数：检查是否是API密钥或网络错误
func isAPIKeyOrNetworkError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "api key") ||
		contains(errStr, "unauthorized") ||
		contains(errStr, "authentication") ||
		contains(errStr, "network") ||
		contains(errStr, "connection") ||
		contains(errStr, "timeout")
}

// 辅助函数：检查是否是已知的API问题
func isKnownAPIIssue(errMsg string) bool {
	return contains(errMsg, "api key") ||
		contains(errMsg, "unauthorized") ||
		contains(errMsg, "rate limit") ||
		contains(errMsg, "quota") ||
		contains(errMsg, "service unavailable") ||
		contains(errMsg, "payment required") ||
		contains(errMsg, "requires more credits") ||
		contains(errMsg, "more credits") ||
		contains(errMsg, "insufficient") ||
		contains(errMsg, "can only afford") ||
		contains(errMsg, "max_tokens") ||
		contains(errMsg, "402")
}

// 辅助函数：字符串包含检查（不区分大小写）
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
