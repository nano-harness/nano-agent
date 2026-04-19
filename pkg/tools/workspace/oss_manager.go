package workspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// OSSManagerTool implements Alibaba Cloud OSS file upload and download functionality
type OSSManagerTool struct {
	workingDir string
	config     *config.OSSConfig
	client     *oss.Client
	bucket     *oss.Bucket
}

// NewOSSManagerTool creates a new OSSManagerTool instance
func NewOSSManagerTool(workingDir string, _ map[string]interface{}) *OSSManagerTool {
	// Get OSS config from global config
	globalCfg := config.Get()
	var ossConfig *config.OSSConfig
	if globalCfg != nil {
		ossConfig = globalCfg.OSS
	}
	if ossConfig == nil {
		ossConfig = &config.OSSConfig{
			Timeout: 30,
			Enabled: false,
		}
	}
	return &OSSManagerTool{
		workingDir: workingDir,
		config:     ossConfig,
	}
}

// Name returns the tool name
func (t *OSSManagerTool) Name() string {
	return "oss_manager"
}

// Description returns the tool description
func (t *OSSManagerTool) Description() string {
	return "Manage Alibaba Cloud OSS files: upload, download, list, and delete objects"
}

// Category returns the tool category
func (t *OSSManagerTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryFileSystem
}

// RequiresConfirmation checks if the tool requires confirmation
func (t *OSSManagerTool) RequiresConfirmation() bool {
	return true // Cloud operations can be costly
}

// ConcurrencySafe returns false: OSS operations have remote side effects.
func (t *OSSManagerTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *OSSManagerTool) Schema() *interfaces.ToolSchema {
	actionProp := interfaces.NewStringPropertyWithEnum(
		"OSS action to perform",
		[]string{"upload", "download", "list", "delete", "info", "configure"},
	)
	actionProp.Examples = []string{"upload", "download", "list", "delete"}
	actionProp.Usage = "upload: Upload local file, base64 image, or image URL to OSS; download: Download file from OSS; list: List objects in bucket; delete: Delete object from OSS; info: Get object info; configure: Set OSS credentials"

	endpointProp := interfaces.NewStringProperty("OSS endpoint (e.g., oss-cn-hangzhou.aliyuncs.com)")
	endpointProp.Examples = []string{"oss-cn-hangzhou.aliyuncs.com", "oss-cn-beijing.aliyuncs.com", "oss-cn-shanghai.aliyuncs.com"}
	endpointProp.Usage = "Alibaba Cloud OSS endpoint URL for your region. Required for configure action."

	accessKeyIDProp := interfaces.NewStringProperty("Access Key ID for OSS authentication")
	accessKeyIDProp.Examples = []string{"LTAI4G..."}
	accessKeyIDProp.Usage = "Alibaba Cloud Access Key ID. Required for configure action."

	accessKeySecretProp := interfaces.NewStringProperty("Access Key Secret for OSS authentication")
	accessKeySecretProp.Examples = []string{"xxx..."}
	accessKeySecretProp.Usage = "Alibaba Cloud Access Key Secret. Required for configure action."

	localFileProp := interfaces.NewStringProperty("Local file path for upload/download")
	localFileProp.Examples = []string{"./document.pdf", "/home/user/image.jpg", "./src/main.go"}
	localFileProp.Usage = "Path to local file for upload or target path for download."

	ossKeyProp := interfaces.NewStringProperty("OSS object key (path in bucket)")
	ossKeyProp.Examples = []string{"documents/file.pdf", "images/photo.jpg", "code/main.go"}
	ossKeyProp.Usage = "Object key (path) in OSS bucket. Used for upload, download, delete, and info operations."

	prefixProp := interfaces.NewStringProperty("Prefix for listing objects (optional)")
	prefixProp.Examples = []string{"documents/", "images/2023/", "backups/"}
	prefixProp.Usage = "Prefix to filter objects when listing. Leave empty to list all objects."

	maxKeysProp := interfaces.NewStringProperty("Maximum number of objects to list (default: 100)")
	maxKeysProp.Examples = []string{"10", "50", "100", "1000"}
	maxKeysProp.Usage = "Maximum number of objects to return when listing. Default is 100."

	// New properties for image uploads
	imageBase64Prop := interfaces.NewStringProperty("Base64 encoded image data or data URL")
	imageBase64Prop.Examples = []string{"iVBORw0KGgoAAA...", "data:image/png;base64,iVBORw0KGgoAAA..."}
	imageBase64Prop.Usage = "Use when the model returns base64 image data; uploads directly without saving locally."

	imageURLProp := interfaces.NewStringProperty("Image URL for streaming upload")
	imageURLProp.Examples = []string{"https://example.com/path/to/image.png"}
	imageURLProp.Usage = "If provided, the tool streams the image from HTTP to OSS without storing locally."

	return interfaces.CreateSchema(
		"Manage Alibaba Cloud OSS files and objects",
		map[string]*interfaces.PropertySchema{
			"action":            actionProp,
			"endpoint":          endpointProp,
			"access_key_id":     accessKeyIDProp,
			"access_key_secret": accessKeySecretProp,
			"local_file":        localFileProp,
			"oss_key":           ossKeyProp,
			"prefix":            prefixProp,
			"max_keys":          maxKeysProp,
			"image_base64":      imageBase64Prop,
			"image_url":         imageURLProp,
		},
		[]string{"action"},
	)
}

// Execute executes the OSS manager tool
func (t *OSSManagerTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to execute OSS operation: tool parameters are missing",
			LLMContent:  "oss_manager failed: tool parameters are missing",
		}, nil
	}

	// Extract action
	action, ok := params["action"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "action parameter is required and must be a string",
			UserContent: "❌ Failed to execute OSS operation: action parameter is required",
			LLMContent:  "oss_manager failed: action parameter is required",
		}, nil
	}

	switch action {
	case "configure":
		return t.configure(ctx, params)
	case "upload":
		return t.uploadFile(ctx, params)
	case "download":
		return t.downloadFile(ctx, params)
	case "list":
		return t.listObjects(ctx, params)
	case "delete":
		return t.deleteObject(ctx, params)
	case "info":
		return t.getObjectInfo(ctx, params)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported action: %s", action),
			UserContent: fmt.Sprintf("❌ Unsupported OSS action: %s", action),
			LLMContent:  fmt.Sprintf("oss_manager failed: unsupported action %s", action),
		}, nil
	}
}

func (t *OSSManagerTool) configure(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	endpoint, ok := params["endpoint"].(string)
	if !ok || endpoint == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "endpoint is required for configure action",
			UserContent: "❌ OSS endpoint is required for configuration",
			LLMContent:  "oss_manager configure failed: endpoint is required",
		}, nil
	}

	t.config.Endpoint = endpoint
	accessKeyID, ok := params["access_key_id"].(string)
	if !ok || accessKeyID == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "access_key_id is required for configure action",
			UserContent: "❌ Access Key ID is required for configuration",
			LLMContent:  "oss_manager configure failed: access_key_id is required",
		}, nil
	}

	accessKeySecret, ok := params["access_key_secret"].(string)
	if !ok || accessKeySecret == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "access_key_secret is required for configure action",
			UserContent: "❌ Access Key Secret is required for configuration",
			LLMContent:  "oss_manager configure failed: access_key_secret is required",
		}, nil
	}

	// Create OSS client
	client, err := oss.New(t.config.NormalizedEndpoint(), accessKeyID, accessKeySecret)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create OSS client: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to create OSS client: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager configure failed: %v", err),
		}, nil
	}

	t.client = client
	// Store configuration (without sensitive data in response)
	t.config.AccessKeyID = accessKeyID
	t.config.AccessKeySecret = accessKeySecret
	t.config.Enabled = true

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"endpoint":      endpoint,
			"access_key_id": accessKeyID[:8] + "...", // Mask sensitive data
			"configured":    true,
		},
		UserContent: fmt.Sprintf("✅ OSS client configured successfully for endpoint: %s", endpoint),
		LLMContent:  fmt.Sprintf("oss_manager configure successful: configured for endpoint %s", endpoint),
	}, nil
}

func (t *OSSManagerTool) ensureConfigured() error {
	// Initialize OSS client if not already done
	if t.client == nil {
		if !t.config.Enabled {
			return fmt.Errorf("OSS is not enabled")
		}

		if t.config.AccessKeyID == "" {
			return fmt.Errorf("OSS access_key_id not configured")
		}

		if t.config.AccessKeySecret == "" {
			return fmt.Errorf("OSS access_key_secret not configured")
		}

		if t.config.Endpoint == "" {
			return fmt.Errorf("OSS endpoint not configured")
		}

		if t.config.DefaultBucket == "" {
			return fmt.Errorf("OSS default bucket not configured")
		}

		client, err := oss.New(t.config.NormalizedEndpoint(), t.config.AccessKeyID, t.config.AccessKeySecret)
		if err != nil {
			return fmt.Errorf("failed to create OSS client: %v", err)
		}
		t.client = client

		bucket, err := client.Bucket(t.config.DefaultBucket)
		if err != nil {
			return fmt.Errorf("failed to get OSS bucket: %v", err)
		}
		t.bucket = bucket
	}
	return nil
}

func (t *OSSManagerTool) getBucket() (*oss.Bucket, error) {
	if err := t.ensureConfigured(); err != nil {
		return nil, err
	}

	// Return the default bucket
	return t.bucket, nil
}

func (t *OSSManagerTool) uploadFile(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	ossKey, ok := params["oss_key"].(string)
	if !ok || ossKey == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "oss_key is required for upload action",
			UserContent: "❌ OSS key is required for upload",
			LLMContent:  "oss_manager upload failed: oss_key is required",
		}, nil
	}

	// Check for image_base64 parameter
	if imageBase64, ok := params["image_base64"].(string); ok && strings.TrimSpace(imageBase64) != "" {
		return t.uploadImageBase64(ctx, ossKey, imageBase64)
	}

	// Check for image_url parameter
	if imageURL, ok := params["image_url"].(string); ok && strings.TrimSpace(imageURL) != "" {
		return t.uploadImageURL(ctx, ossKey, imageURL)
	}

	// Original local file upload logic
	localFile, ok := params["local_file"].(string)
	if !ok || localFile == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "local_file, image_base64, or image_url is required for upload action",
			UserContent: "❌ Local file path, base64 image data, or image URL is required for upload",
			LLMContent:  "oss_manager upload failed: no valid input provided",
		}, nil
	}

	// Convert to absolute path
	absLocalFile, err := filepath.Abs(localFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to resolve local file path: %v", err),
			UserContent: "❌ Failed to resolve local file path",
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	// Check if file exists
	fileInfo, err := os.Stat(absLocalFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("local file not found: %v", err),
			UserContent: fmt.Sprintf("❌ Local file not found: %s", absLocalFile),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	// Upload file
	start := time.Now()
	err = bucket.PutObjectFromFile(ossKey, absLocalFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to upload file: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to upload file: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	duration := time.Since(start)
	fileSize := fileInfo.Size()

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name": t.config.DefaultBucket,
			"local_file":  absLocalFile,
			"oss_key":     ossKey,
			"file_size":   fileSize,
			"duration":    duration.String(),
			"upload_time": time.Now().Format(time.RFC3339),
		},
		UserContent: fmt.Sprintf("✅ File uploaded successfully\n  📁 Local: %s\n  ☁️ OSS: %s/%s\n  📊 Size: %d bytes\n  ⏱️ Duration: %s", absLocalFile, t.config.DefaultBucket, ossKey, fileSize, duration),
		LLMContent:  fmt.Sprintf("oss_manager upload successful: uploaded %s to %s/%s (%d bytes)", absLocalFile, t.config.DefaultBucket, ossKey, fileSize),
	}, nil
}

// uploadImageBase64 handles base64 image upload
func (t *OSSManagerTool) uploadImageBase64(_ context.Context, ossKey, imageBase64 string) (*interfaces.ToolResult, error) {
	// Parse data URL if present
	var payload string
	var contentType = "image/png" // default

	imageBase64 = strings.TrimSpace(imageBase64)
	if strings.HasPrefix(imageBase64, "data:") {
		comma := strings.Index(imageBase64, ",")
		if comma > 0 {
			header := imageBase64[:comma]
			payload = imageBase64[comma+1:]
			if strings.Contains(header, ";base64") {
				parts := strings.Split(header, ";")
				if len(parts) > 0 {
					contentType = strings.TrimPrefix(parts[0], "data:")
				}
			} else {
				contentType = strings.TrimPrefix(header, "data:")
			}
		} else {
			payload = imageBase64
		}
	} else {
		payload = imageBase64
	}

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       fmt.Sprintf("failed to decode base64 image: %v", err),
				UserContent: "❌ Failed to decode base64 image data",
				LLMContent:  fmt.Sprintf("oss_manager upload failed: base64 decode error: %v", err),
			}, nil
		}
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	// Upload to OSS
	start := time.Now()
	var opts []oss.Option
	if contentType != "" {
		opts = append(opts, oss.ContentType(contentType))
	}

	err = bucket.PutObject(ossKey, bytes.NewReader(decoded), opts...)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to upload base64 image: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to upload base64 image: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	duration := time.Since(start)
	fileSize := int64(len(decoded))

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name":  t.config.DefaultBucket,
			"oss_key":      ossKey,
			"file_size":    fileSize,
			"content_type": contentType,
			"duration":     duration.String(),
			"upload_time":  time.Now().Format(time.RFC3339),
			"source":       "base64",
		},
		UserContent: fmt.Sprintf("✅ Base64 image uploaded successfully\n  ☁️ OSS: %s/%s\n  📊 Size: %d bytes\n  🎨 Type: %s\n  ⏱️ Duration: %s", t.config.DefaultBucket, ossKey, fileSize, contentType, duration),
		LLMContent:  fmt.Sprintf("oss_manager upload successful: uploaded base64 image to %s/%s (%d bytes, %s)", t.config.DefaultBucket, ossKey, fileSize, contentType),
	}, nil
}

// uploadImageURL handles image URL upload
func (t *OSSManagerTool) uploadImageURL(ctx context.Context, ossKey, imageURL string) (*interfaces.ToolResult, error) {
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create HTTP request: %v", err),
			UserContent: "❌ Failed to create HTTP request for image URL",
			LLMContent:  fmt.Sprintf("oss_manager upload failed: HTTP request error: %v", err),
		}, nil
	}

	// Download image
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to download image: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to download image from URL: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: download error: %v", err),
		}, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("HTTP error: %d", resp.StatusCode),
			UserContent: fmt.Sprintf("❌ Failed to download image: HTTP %d", resp.StatusCode),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: HTTP %d", resp.StatusCode),
		}, nil
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	// Upload to OSS
	start := time.Now()
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	var opts []oss.Option
	if contentType != "" {
		opts = append(opts, oss.ContentType(contentType))
	}

	err = bucket.PutObject(ossKey, resp.Body, opts...)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to upload image from URL: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to upload image from URL: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager upload failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name":  t.config.DefaultBucket,
			"oss_key":      ossKey,
			"source_url":   imageURL,
			"content_type": contentType,
			"duration":     duration.String(),
			"upload_time":  time.Now().Format(time.RFC3339),
			"source":       "url",
		},
		UserContent: fmt.Sprintf("✅ Image from URL uploaded successfully\n  🌐 Source: %s\n  ☁️ OSS: %s/%s\n  🎨 Type: %s\n  ⏱️ Duration: %s", imageURL, t.config.DefaultBucket, ossKey, contentType, duration),
		LLMContent:  fmt.Sprintf("oss_manager upload successful: uploaded image from %s to %s/%s (%s)", imageURL, t.config.DefaultBucket, ossKey, contentType),
	}, nil
}

func (t *OSSManagerTool) downloadFile(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	ossKey, ok := params["oss_key"].(string)
	if !ok || ossKey == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "oss_key is required for download action",
			UserContent: "❌ OSS key is required for download",
			LLMContent:  "oss_manager download failed: oss_key is required",
		}, nil
	}

	localFile, ok := params["local_file"].(string)
	if !ok || localFile == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "local_file is required for download action",
			UserContent: "❌ Local file path is required for download",
			LLMContent:  "oss_manager download failed: local_file is required",
		}, nil
	}

	// Convert to absolute path
	absLocalFile, err := filepath.Abs(localFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to resolve local file path: %v", err),
			UserContent: "❌ Failed to resolve local file path",
			LLMContent:  fmt.Sprintf("oss_manager download failed: %v", err),
		}, nil
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(absLocalFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create directory: %v", err),
			UserContent: "❌ Failed to create target directory",
			LLMContent:  fmt.Sprintf("oss_manager download failed: %v", err),
		}, nil
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager download failed: %v", err),
		}, nil
	}

	// Download file
	start := time.Now()
	err = bucket.GetObjectToFile(ossKey, absLocalFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to download file: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to download file: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager download failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	// Get file info
	fileInfo, err := os.Stat(absLocalFile)
	var fileSize int64
	if err == nil {
		fileSize = fileInfo.Size()
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name":   t.config.DefaultBucket,
			"oss_key":       ossKey,
			"local_file":    absLocalFile,
			"file_size":     fileSize,
			"duration":      duration.String(),
			"download_time": time.Now().Format(time.RFC3339),
		},
		UserContent: fmt.Sprintf("✅ File downloaded successfully\n  ☁️ OSS: %s/%s\n  📁 Local: %s\n  📊 Size: %d bytes\n  ⏱️ Duration: %s", t.config.DefaultBucket, ossKey, absLocalFile, fileSize, duration),
		LLMContent:  fmt.Sprintf("oss_manager download successful: downloaded %s/%s to %s (%d bytes)", t.config.DefaultBucket, ossKey, absLocalFile, fileSize),
	}, nil
}

func (t *OSSManagerTool) listObjects(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Get optional parameters
	prefix := ""
	if prefixParam, ok := params["prefix"].(string); ok {
		prefix = prefixParam
	}

	maxKeys := 100
	if maxKeysParam, ok := params["max_keys"].(float64); ok {
		maxKeys = int(maxKeysParam)
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager list failed: %v", err),
		}, nil
	}

	// List objects
	options := []oss.Option{
		oss.MaxKeys(maxKeys),
	}
	if prefix != "" {
		options = append(options, oss.Prefix(prefix))
	}

	lor, err := bucket.ListObjects(options...)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to list objects: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to list objects: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager list failed: %v", err),
		}, nil
	}

	// Process results
	var objects []map[string]interface{}
	var totalSize int64

	for _, obj := range lor.Objects {
		totalSize += obj.Size
		objects = append(objects, map[string]interface{}{
			"key":           obj.Key,
			"size":          obj.Size,
			"last_modified": obj.LastModified.Format(time.RFC3339),
			"etag":          obj.ETag,
			"storage_class": obj.StorageClass,
		})
	}

	// Format user content
	userContent := fmt.Sprintf("📋 Objects in bucket '%s'\n", t.config.DefaultBucket)
	if prefix != "" {
		userContent += fmt.Sprintf("🔍 Prefix: %s\n", prefix)
	}
	userContent += fmt.Sprintf("📊 Found %d objects (Total size: %d bytes)\n\n", len(objects), totalSize)

	for i, obj := range objects {
		if i >= 20 { // Limit display to first 20 objects
			userContent += fmt.Sprintf("... and %d more objects\n", len(objects)-20)
			break
		}
		userContent += fmt.Sprintf("  📄 %s (%d bytes, %s)\n", obj["key"], obj["size"], obj["last_modified"])
	}

	if len(objects) == 0 {
		userContent += "  (No objects found)\n"
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name": t.config.DefaultBucket,
			"prefix":      prefix,
			"max_keys":    maxKeys,
			"objects":     objects,
			"count":       len(objects),
			"total_size":  totalSize,
			"truncated":   lor.IsTruncated,
		},
		UserContent: userContent,
		LLMContent:  fmt.Sprintf("oss_manager list successful: found %d objects in %s (total size: %d bytes)", len(objects), t.config.DefaultBucket, totalSize),
	}, nil
}

func (t *OSSManagerTool) deleteObject(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	ossKey, ok := params["oss_key"].(string)
	if !ok || ossKey == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "oss_key is required for delete action",
			UserContent: "❌ OSS key is required for delete",
			LLMContent:  "oss_manager delete failed: oss_key is required",
		}, nil
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager delete failed: %v", err),
		}, nil
	}

	// Delete object
	err = bucket.DeleteObject(ossKey)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to delete object: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to delete object: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager delete failed: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name": t.config.DefaultBucket,
			"oss_key":     ossKey,
			"deleted_at":  time.Now().Format(time.RFC3339),
		},
		UserContent: fmt.Sprintf("✅ Object deleted successfully: %s/%s", t.config.DefaultBucket, ossKey),
		LLMContent:  fmt.Sprintf("oss_manager delete successful: deleted %s/%s", t.config.DefaultBucket, ossKey),
	}, nil
}

func (t *OSSManagerTool) getObjectInfo(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	ossKey, ok := params["oss_key"].(string)
	if !ok || ossKey == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "oss_key is required for info action",
			UserContent: "❌ OSS key is required for info",
			LLMContent:  "oss_manager info failed: oss_key is required",
		}, nil
	}

	// Get bucket
	bucket, err := t.getBucket()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Failed to access bucket: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager info failed: %v", err),
		}, nil
	}

	// Get object metadata
	header, err := bucket.GetObjectMeta(ossKey)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to get object info: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to get object info: %v", err),
			LLMContent:  fmt.Sprintf("oss_manager info failed: %v", err),
		}, nil
	}

	// Extract metadata
	contentLength := header.Get("Content-Length")
	contentType := header.Get("Content-Type")
	lastModified := header.Get("Last-Modified")
	etag := header.Get("ETag")

	userContent := fmt.Sprintf("ℹ️ Object Information: %s/%s\n", t.config.DefaultBucket, ossKey)
	userContent += fmt.Sprintf("  📊 Size: %s bytes\n", contentLength)
	userContent += fmt.Sprintf("  📄 Type: %s\n", contentType)
	userContent += fmt.Sprintf("  🕒 Last Modified: %s\n", lastModified)
	userContent += fmt.Sprintf("  🔖 ETag: %s\n", etag)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"bucket_name":    t.config.DefaultBucket,
			"oss_key":        ossKey,
			"content_length": contentLength,
			"content_type":   contentType,
			"last_modified":  lastModified,
			"etag":           etag,
			"headers":        header,
		},
		UserContent: userContent,
		LLMContent:  fmt.Sprintf("oss_manager info successful: retrieved info for %s/%s", t.config.DefaultBucket, ossKey),
	}, nil
}
