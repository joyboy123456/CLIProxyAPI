// Package executor provides runtime execution capabilities for various AI service providers.
package executor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

// imageHostingResponse represents the response from PixelPunk image hosting API.
// PixelPunk returns: {"code":200,"data":{"uploaded":{"url":"..."}}}
type imageHostingResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Uploaded struct {
			ID       string `json:"id"`
			URL      string `json:"url"`
			ThumbURL string `json:"thumb_url"`
		} `json:"uploaded"`
	} `json:"data"`
}

// UploadBase64Image uploads a base64-encoded image to the configured image hosting service
// and returns the public URL. If image hosting is not enabled or fails, it returns the original URL.
//
// Parameters:
//   - cfg: The application configuration containing image hosting settings
//   - imageURL: The image URL, which can be a data URL (data:image/...;base64,...) or a regular URL
//
// Returns:
//   - The public URL if upload succeeds, or the original URL if not applicable
//   - An error if the upload fails
func UploadBase64Image(cfg *config.Config, imageURL string) (string, error) {
	// Check if image hosting is enabled
	if cfg == nil || !cfg.ImageHosting.Enable || cfg.ImageHosting.Endpoint == "" {
		return imageURL, nil
	}

	// Only process data URLs (base64 encoded images)
	if !strings.HasPrefix(imageURL, "data:") {
		return imageURL, nil
	}

	// Parse the data URL: data:[<mediatype>][;base64],<data>
	mimeType, base64Data, err := parseDataURL(imageURL)
	if err != nil {
		return imageURL, fmt.Errorf("failed to parse data URL: %w", err)
	}

	// Decode base64 data
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return imageURL, fmt.Errorf("failed to decode base64 image: %w", err)
	}

	// Determine file extension from mime type
	ext := getExtensionFromMimeType(mimeType)
	filename := fmt.Sprintf("upload_%d%s", time.Now().UnixNano(), ext)

	// Create multipart form data
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add the file part
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return imageURL, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err = part.Write(imageData); err != nil {
		return imageURL, fmt.Errorf("failed to write image data: %w", err)
	}

	// Add optional parameters
	_ = writer.WriteField("access_level", "public")
	_ = writer.WriteField("optimize", "true")

	if err = writer.Close(); err != nil {
		return imageURL, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodPost, cfg.ImageHosting.Endpoint, &body)
	if err != nil {
		return imageURL, fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-pixelpunk-key", cfg.ImageHosting.APIKey)

	// Execute the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return imageURL, fmt.Errorf("failed to upload image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return imageURL, fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return imageURL, fmt.Errorf("image upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result imageHostingResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		log.Warnf("image hosting: failed to parse JSON response, raw: %s", string(respBody))
		return imageURL, fmt.Errorf("failed to parse upload response: %w", err)
	}

	// Check for success (code 200)
	if result.Code != 200 {
		return imageURL, fmt.Errorf("image upload failed: %s", result.Message)
	}

	// Get the uploaded URL
	publicURL := result.Data.Uploaded.URL
	if publicURL == "" {
		return imageURL, fmt.Errorf("image upload response missing URL")
	}

	log.Infof("image hosting: uploaded image successfully, public URL: %s", publicURL)
	return publicURL, nil
}

// parseDataURL parses a data URL and returns the MIME type and base64 data.
// Format: data:[<mediatype>][;base64],<data>
func parseDataURL(dataURL string) (mimeType, data string, err error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", fmt.Errorf("not a data URL")
	}

	// Remove "data:" prefix
	rest := dataURL[5:]

	// Find the comma separator
	commaIdx := strings.Index(rest, ",")
	if commaIdx == -1 {
		return "", "", fmt.Errorf("invalid data URL format: no comma separator")
	}

	metadata := rest[:commaIdx]
	data = rest[commaIdx+1:]

	// Parse metadata (e.g., "image/png;base64")
	parts := strings.Split(metadata, ";")
	if len(parts) >= 1 {
		mimeType = parts[0]
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return mimeType, data, nil
}

// getExtensionFromMimeType returns a file extension based on the MIME type.
func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png" // Default to PNG
	}
}
