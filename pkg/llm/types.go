package llm

// MultimodalImage represents an image that can be sent in a multimodal request
type MultimodalImage struct {
	URL      string `json:"url,omitempty"`
	Base64   string `json:"base64,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}
