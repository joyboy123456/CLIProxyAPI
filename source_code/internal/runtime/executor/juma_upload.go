// Package executor provides runtime execution capabilities for various AI service providers.
package executor

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	log "github.com/sirupsen/logrus"
)

// JumaImageUploadResult contains the result of uploading an image to Juma
type JumaImageUploadResult struct {
	ID              string `json:"id"`              // This is the image.id (for backwards compat)
	KnowledgeItemID string `json:"knowledgeItemId"` // This is the ID needed for knowledgeItems
	ImageURL        string `json:"imageUrl"`
	Name            string `json:"name"`
}

// UploadImageToJuma uploads a base64-encoded image to Juma's file storage
// and returns the Juma-hosted image URL.
//
// This function handles the full upload flow:
// 1. Parse the data URL to get mime type and binary data
// 2. Call Juma's fileStorage.createPresignedUrl to get S3 upload credentials
// 3. Upload the image to S3 using the presigned URL
// 4. Return the Juma-hosted image URL for use in chat
func UploadImageToJuma(sessionToken, workspaceID, imageDataURL string) (*JumaImageUploadResult, error) {
	// Only process data URLs
	if !strings.HasPrefix(imageDataURL, "data:") {
		return nil, fmt.Errorf("not a data URL")
	}

	// Parse the data URL
	mimeType, base64Data, err := parseJumaDataURL(imageDataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data URL: %w", err)
	}

	// Decode base64 data
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Generate filename
	ext := getJumaExtensionFromMimeType(mimeType)
	filename := fmt.Sprintf("upload_%d%s", time.Now().UnixNano(), ext)

	// Step 1: Get presigned URL from Juma
	presignedData, err := getJumaPresignedURL(sessionToken, workspaceID, filename, mimeType, len(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to get presigned URL: %w", err)
	}

	// Step 2: Upload to S3
	if err := uploadToJumaS3(presignedData, imageData, mimeType); err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	log.Infof("juma upload: uploaded image successfully, URL: %s, KnowledgeItemID: %s", presignedData.ImageURL, presignedData.KnowledgeItemID)

	return &JumaImageUploadResult{
		ID:              presignedData.ImageID,
		KnowledgeItemID: presignedData.KnowledgeItemID,
		ImageURL:        presignedData.ImageURL,
		Name:            filename,
	}, nil
}

type jumaPresignedData struct {
	ImageID          string
	KnowledgeItemID  string // This is the ID needed for knowledgeItems in chat request
	ImageURL         string
	PresignedURL     string
	Fields           map[string]string
}

func getJumaPresignedURL(sessionToken, workspaceID, filename, mimeType string, imageSize int) (*jumaPresignedData, error) {
	url := "https://app.juma.ai/api/trpc/fileStorage.createPresignedUrl?batch=1"

	payload := map[string]any{
		"0": map[string]any{
			"json": map[string]any{
				"type":      "Knowledge",
				"threadId":  nil,
				"name":      filename,
				"mimeType":  mimeType,
				"imageSize": imageSize,
			},
			"meta": map[string]any{
				"values": map[string]any{
					"threadId": []string{"undefined"},
				},
				"v": 1,
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://app.juma.ai")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("x-workspace-id", workspaceID)
	req.Header.Set("trpc-accept", "application/jsonl")
	req.Header.Set("x-trpc-source", "web")
	req.AddCookie(&http.Cookie{
		Name:  "__Secure-next-auth.session-token",
		Value: sessionToken,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("presigned URL request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSONL response - find the line with presignedUrl
	scanner := bufio.NewScanner(resp.Body)
	var presignedData *jumaPresignedData

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "presignedUrl") {
			// Log full response for debugging
			log.Debugf("juma upload: presigned URL response line: %s", line)
			
			// Extract data from the JSONL response
			jsonResult := gjson.Parse(line)
			
			// Navigate to the data: json[2][0][0] has the image and presignedUrl
			imageData := jsonResult.Get("json.2.0.0")
			if !imageData.Exists() {
				continue
			}

			imageID := imageData.Get("image.id").String()
			imageURL := imageData.Get("image.imageUrl").String()
			presignedURL := imageData.Get("presignedUrl").String()
			
			// Extract knowledgeItem.id - this is the ID we need for the chat API
			knowledgeItemID := imageData.Get("knowledgeItem.id").String()
			log.Infof("juma upload: extracted IDs - imageID=%s, knowledgeItemID=%s", imageID, knowledgeItemID)
			
			if imageURL == "" || presignedURL == "" {
				continue
			}

			// Extract fields
			fields := make(map[string]string)
			imageData.Get("fields").ForEach(func(key, value gjson.Result) bool {
				fields[key.String()] = value.String()
				return true
			})

			presignedData = &jumaPresignedData{
				ImageID:         imageID,
				KnowledgeItemID: knowledgeItemID,
				ImageURL:        imageURL,
				PresignedURL:    presignedURL,
				Fields:          fields,
			}
			break
		}
	}

	if presignedData == nil {
		return nil, fmt.Errorf("failed to parse presigned URL response")
	}

	return presignedData, nil
}

func uploadToJumaS3(presignedData *jumaPresignedData, imageData []byte, mimeType string) error {
	// Create multipart form data for S3 upload
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Log all fields for debugging
	log.Infof("juma S3 upload: presignedURL=%s", presignedData.PresignedURL)
	for k, v := range presignedData.Fields {
		log.Infof("juma S3 upload: field %s = %s", k, v)
	}

	// S3 presigned POST requires specific field order:
	// 1. key (the file path in S3)
	// 2. Content-Type
	// 3. Other policy fields (bucket, X-Amz-Algorithm, etc.)
	// 4. Policy
	// 5. X-Amz-Signature
	// 6. file (must be last)

	// Order matters! Write fields in the order they appear in the policy
	fieldOrder := []string{"key", "Content-Type", "bucket", "X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "Policy", "X-Amz-Signature"}
	
	for _, fieldName := range fieldOrder {
		if value, exists := presignedData.Fields[fieldName]; exists {
			if err := writer.WriteField(fieldName, value); err != nil {
				return fmt.Errorf("failed to write field %s: %w", fieldName, err)
			}
		}
	}

	// Add any remaining fields not in our predefined order
	for key, value := range presignedData.Fields {
		found := false
		for _, ordered := range fieldOrder {
			if key == ordered {
				found = true
				break
			}
		}
		if !found {
			if err := writer.WriteField(key, value); err != nil {
				return fmt.Errorf("failed to write field %s: %w", key, err)
			}
		}
	}

	// Add the file part last - this is REQUIRED by S3 presigned POST
	// CRITICAL: Use CreatePart with explicit MIMEHeader to set the correct Content-Type
	// CreateFormFile uses "application/octet-stream" which doesn't match the S3 policy
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="image.png"`)
	h.Set("Content-Type", mimeType) // Must match the Content-Type field in the S3 policy
	part, err := writer.CreatePart(h)
	if err != nil {
		return fmt.Errorf("failed to create file part: %w", err)
	}
	if _, err := part.Write(imageData); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, presignedData.PresignedURL, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("S3 upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func parseJumaDataURL(dataURL string) (mimeType, data string, err error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", fmt.Errorf("not a data URL")
	}

	rest := dataURL[5:]
	commaIdx := strings.Index(rest, ",")
	if commaIdx == -1 {
		return "", "", fmt.Errorf("invalid data URL format")
	}

	metadata := rest[:commaIdx]
	data = rest[commaIdx+1:]

	parts := strings.Split(metadata, ";")
	if len(parts) >= 1 {
		mimeType = parts[0]
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return mimeType, data, nil
}

func getJumaExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
