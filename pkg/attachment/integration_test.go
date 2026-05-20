package attachment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// TestAttachmentWorkflowIntegration tests the complete workflow of saving an image
// and converting it to a multimodal format that can be sent to LLM
func TestAttachmentWorkflowIntegration(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Step 1: Create attachment manager
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create attachment manager: %v", err)
	}

	// Step 2: Simulate pasting an image (using test PNG data)
	testImageData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1 pixel
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // color type RGB
		0xDE, // CRC
	}

	imagePath, err := mgr.SaveImage(testImageData, "image/png")
	if err != nil {
		t.Fatalf("Failed to save image: %v", err)
	}

	// Step 3: Verify image was saved
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		t.Errorf("Image file was not created: %s", imagePath)
	}

	// Step 4: Convert to multimodal format
	multimodalImg, err := mgr.ToMultimodalImage(imagePath)
	if err != nil {
		t.Fatalf("Failed to convert to multimodal image: %v", err)
	}

	// Step 5: Verify multimodal image structure
	if multimodalImg.URL == "" {
		t.Error("Multimodal image URL is empty")
	}

	if multimodalImg.MimeType != "image/png" {
		t.Errorf("Expected MIME type image/png, got %s", multimodalImg.MimeType)
	}

	// The URL should be a data URL with base64 encoding
	expectedPrefix := "data:image/png;base64,"
	if len(multimodalImg.URL) < len(expectedPrefix) {
		t.Errorf("Multimodal image URL too short: %d characters", len(multimodalImg.URL))
	} else if multimodalImg.URL[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("Expected data URL to start with %s, got %s", expectedPrefix, multimodalImg.URL[:len(expectedPrefix)])
	}

	// Step 6: Verify base64 data is present and non-empty
	base64Data := multimodalImg.URL[len(expectedPrefix):]
	if len(base64Data) == 0 {
		t.Error("Base64 encoded data is empty")
	}

	t.Logf("Successfully created multimodal image: %d bytes of base64 data", len(base64Data))
}

// TestFileReferenceWorkflow tests the @file reference workflow
func TestFileReferenceWorkflow(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test image file
	testImagePath := filepath.Join(tmpDir, "test_image.jpg")
	testData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG header
	if err := os.WriteFile(testImagePath, testData, 0644); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create attachment manager: %v", err)
	}

	// Step 1: Parse @file reference from user input
	userInput := "Please analyze @" + testImagePath + " and tell me what you see"
	fileRefs := ParseFileReference(userInput)

	if len(fileRefs) != 1 {
		t.Fatalf("Expected 1 file reference, got %d", len(fileRefs))
	}

	if fileRefs[0] != testImagePath {
		t.Errorf("Expected file path %s, got %s", testImagePath, fileRefs[0])
	}

	// Step 2: Verify it's an image file
	if !IsImageFile(fileRefs[0]) {
		t.Error("Expected file to be recognized as an image")
	}

	// Step 3: Convert to multimodal format
	multimodalImg, err := mgr.ToMultimodalImage(fileRefs[0])
	if err != nil {
		t.Fatalf("Failed to convert file reference to multimodal: %v", err)
	}

	// Step 4: Verify the multimodal image
	if multimodalImg.MimeType != "image/jpeg" {
		t.Errorf("Expected MIME type image/jpeg, got %s", multimodalImg.MimeType)
	}

	if multimodalImg.URL == "" {
		t.Error("Multimodal image URL is empty")
	}
}

// TestMultipleImagesWorkflow tests handling multiple images in one submission
func TestMultipleImagesWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create attachment manager: %v", err)
	}

	// Simulate user pasting 2 images and referencing 1 file
	var pendingImages []llm.MultimodalImage

	// Image 1: Pasted from clipboard
	img1Data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	img1Path, err := mgr.SaveImage(img1Data, "image/png")
	if err != nil {
		t.Fatalf("Failed to save first image: %v", err)
	}
	img1, err := mgr.ToMultimodalImage(img1Path)
	if err != nil {
		t.Fatalf("Failed to convert first image: %v", err)
	}
	pendingImages = append(pendingImages, img1)

	// Image 2: Pasted from clipboard
	img2Data := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG header
	img2Path, err := mgr.SaveImage(img2Data, "image/jpeg")
	if err != nil {
		t.Fatalf("Failed to save second image: %v", err)
	}
	img2, err := mgr.ToMultimodalImage(img2Path)
	if err != nil {
		t.Fatalf("Failed to convert second image: %v", err)
	}
	pendingImages = append(pendingImages, img2)

	// Image 3: Referenced via @file
	img3Path := filepath.Join(tmpDir, "referenced.gif")
	if err := os.WriteFile(img3Path, []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, 0644); err != nil {
		t.Fatalf("Failed to create referenced image: %v", err)
	}

	userInput := "Compare these images: @" + img3Path
	fileRefs := ParseFileReference(userInput)
	for _, ref := range fileRefs {
		if IsImageFile(ref) {
			img, err := mgr.ToMultimodalImage(ref)
			if err == nil {
				pendingImages = append(pendingImages, img)
			}
		}
	}

	// Verify we have all 3 images
	if len(pendingImages) != 3 {
		t.Fatalf("Expected 3 images, got %d", len(pendingImages))
	}

	// Verify MIME types
	expectedMimes := []string{"image/png", "image/jpeg", "image/gif"}
	for i, img := range pendingImages {
		if img.MimeType != expectedMimes[i] {
			t.Errorf("Image %d: expected MIME type %s, got %s", i, expectedMimes[i], img.MimeType)
		}
	}

	// At this point, pendingImages would be sent via:
	// agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, userInput, pendingImages, callback)
	t.Logf("Successfully prepared %d images for multimodal submission", len(pendingImages))
}

// TestAttachmentCleanup tests the cleanup of old attachments
func TestAttachmentCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create attachment manager: %v", err)
	}

	attachDir := filepath.Join(tmpDir, ".nano", "attachments")

	// Create some test files
	files := []string{"img1.png", "img2.jpg", "img3.gif"}
	for _, file := range files {
		filePath := filepath.Join(attachDir, file)
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// Verify files exist
	entries, err := os.ReadDir(attachDir)
	if err != nil {
		t.Fatalf("Failed to read attachments directory: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Expected 3 files, got %d", len(entries))
	}

	// Clean attachments (all files are new, so none should be deleted with days=1)
	err = mgr.CleanOldAttachments(1)
	if err != nil {
		t.Errorf("CleanOldAttachments failed: %v", err)
	}

	// Files should still exist
	entries, err = os.ReadDir(attachDir)
	if err != nil {
		t.Fatalf("Failed to read attachments directory after cleanup: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Expected 3 files after cleanup, got %d", len(entries))
	}
}
