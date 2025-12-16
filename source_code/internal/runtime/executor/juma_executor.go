// Package executor provides runtime execution capabilities for various AI service providers.
package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	// jumaBaseURL is the base URL for the Juma API.
	jumaBaseURL = "https://app.juma.ai"
	// jumaMaxRemoteImageBytes limits remote image fetch size when converting non-data URLs.
	jumaMaxRemoteImageBytes = 10 << 20 // 10 MiB
)

// JumaExecutor implements a stateless executor for Juma.ai.
// It handles session token authentication and SSE streaming responses.
type JumaExecutor struct {
	cfg *config.Config
}

// NewJumaExecutor creates a new Juma executor instance.
func NewJumaExecutor(cfg *config.Config) *JumaExecutor {
	return &JumaExecutor{cfg: cfg}
}

// Identifier returns the executor identifier for Juma.
func (e *JumaExecutor) Identifier() string { return "juma" }

// PrepareRequest is a no-op for Juma (credentials are added via cookies at execution time).
func (e *JumaExecutor) PrepareRequest(_ *http.Request, _ *cliproxyauth.Auth) error {
	return nil
}

// JumaMessage represents a message in Juma's format.
type JumaMessage struct {
	ID              string            `json:"id"`
	Role            string            `json:"role"`
	Parts           []JumaMessagePart `json:"parts"`
	Content         string            `json:"content"`
	GeneratedImages []any             `json:"generatedImages"`
	UploadedImages  []any             `json:"uploadedImages"`
	UploadedFiles   []any             `json:"uploadedFiles"`
}

// JumaMessagePart represents a part of a Juma message.
// Supports both text and image parts.
type JumaMessagePart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"` // For image parts (URL)
	ImageID  string `json:"imageId,omitempty"`  // For image parts (Juma ID)
}

// JumaRequest represents the request body for Juma chat API.
type JumaRequest struct {
	Messages           []JumaMessage `json:"messages"`
	ModelID            string        `json:"modelId"`
	ThreadID           string        `json:"threadId"`
	WorkspaceID        string        `json:"workspaceId"`
	PromptUsages       []any         `json:"promptUsages"`
	CurrentFolderID    *string       `json:"currentFolderId"`
	VendorConnectionID string        `json:"vendorConnectionId"`
	IsNewThread        bool          `json:"isNewThread"`
	ParentFolderID     *string       `json:"parentFolderId"`
	KnowledgeItems     []any         `json:"knowledgeItems"`
	Tools              []JumaTool    `json:"tools,omitempty"`
}

// JumaTool represents a tool definition for Juma.
type JumaTool struct {
	Type     string           `json:"type"`
	Function JumaToolFunction `json:"function"`
}

// JumaToolFunction represents tool function details.
type JumaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// JumaModel represents a supported Juma model.
type JumaModel struct {
	ID                 string // Juma's internal UUID for the model
	Name               string // Display name (e.g., "GPT-5.1")
	Alias              string // User-facing alias (e.g., "juma-gpt-5.1")
	Provider           string // Vendor type (e.g., "OpenAI", "Gemini")
	VendorConnectionID string // Juma's vendor connection UUID
}

// jumaModels contains the hardcoded list of supported Juma models.
// These model IDs were obtained through API exploration.
var jumaModels = []JumaModel{
	// OpenAI models (vendor: f5275937-68f8-4bfe-b195-c48f2155263b)
	{ID: "401637fa-151b-41f5-aa36-5416ad1314fb", Name: "GPT-5.1", Alias: "juma-gpt-5.1", Provider: "OpenAI", VendorConnectionID: "f5275937-68f8-4bfe-b195-c48f2155263b"},

	// Anthropic models via Bedrock (vendor: f958317e-9359-42cc-8c45-4ed306bf5f65)
	{ID: "790cee03-6d71-4b6a-bf86-7781c9592028", Name: "Claude Opus 4.5", Alias: "juma-claude-opus-4.5", Provider: "Anthropic", VendorConnectionID: "f958317e-9359-42cc-8c45-4ed306bf5f65"},

	// Google AI models (vendor: 2eb35c4f-3afe-4d12-b953-70b5c8bb643e)
	{ID: "c073a0c0-e3d0-4e0b-b36c-29584b674125", Name: "Gemini 3 Pro", Alias: "juma-gemini-3-pro", Provider: "Google", VendorConnectionID: "2eb35c4f-3afe-4d12-b953-70b5c8bb643e"},

	// Nanobanana Pro - Image Editing (Actually Gemini 3 Pro with tool usage)
	// We use the same IDs as Gemini 3 Pro but treat it as a distinct model with forced image editing behavior.
	{ID: "c073a0c0-e3d0-4e0b-b36c-29584b674125", Name: "Nanobanana Pro", Alias: "juma-nanobanana-pro", Provider: "Google", VendorConnectionID: "2eb35c4f-3afe-4d12-b953-70b5c8bb643e"},
}

// getJumaModelByAlias finds a Juma model by its alias.
func getJumaModelByAlias(alias string) *JumaModel {
	for i := range jumaModels {
		if strings.EqualFold(jumaModels[i].Alias, alias) {
			return &jumaModels[i]
		}
	}
	return nil
}

// jumaCredentials extracts session token and IDs from auth.
func jumaCredentials(auth *cliproxyauth.Auth) (sessionToken, workspaceID, vendorConnectionID string) {
	if auth == nil || auth.Attributes == nil {
		return "", "", ""
	}
	sessionToken = strings.TrimSpace(auth.Attributes["session_token"])
	workspaceID = strings.TrimSpace(auth.Attributes["workspace_id"])
	vendorConnectionID = strings.TrimSpace(auth.Attributes["vendor_connection_id"])
	return
}

// JumaUploadedImage represents an uploaded image in Juma's format.
type JumaUploadedImage struct {
	ID       string `json:"id"`
	ImageURL string `json:"imageUrl"`
	Name     string `json:"name"`
}

// JumaConversionResult contains the converted messages and collected image info.
type JumaConversionResult struct {
	Messages       []JumaMessage
	KnowledgeItems []map[string]string // Legacy: for knowledgeItemId if available
	UploadedImages []JumaUploadedImage // New: for direct image attachment via uploadedImages
}

// convertToJumaMessages converts OpenAI-style messages to Juma format.
// Supports both simple string content and array content with text/image_url parts.
// When provided with Juma session credentials, it uploads base64 or remote images to
// Juma storage and collects their knowledge item IDs into KnowledgeItems.
func convertToJumaMessages(cfg *config.Config, payload []byte, sessionToken string, workspaceID string) JumaConversionResult {
	log.Infof("juma executor: convertToJumaMessages called, cfgNil=%v, cfgJumaKeyLen=%d", cfg == nil, func() int {
		if cfg != nil {
			return len(cfg.JumaKey)
		}
		return 0
	}())
	msgs := gjson.GetBytes(payload, "messages").Array()
	result := make([]JumaMessage, 0, len(msgs))
	uploadedImages := make([]JumaUploadedImage, 0)

	// Determine if we need to inject system prompt for Nanobanana
	model := gjson.GetBytes(payload, "model").String()
	if isNanobananaModel(model) {
		systemPrompt := JumaMessage{
			ID:              uuid.New().String(),
			Role:            "system",
			Content:         "You are an expert image editing assistant. When the user provides an image, you MUST use the 'ImageEdit' tool to modify it according to their instructions. Do not just describe the edit. Always output the tool call.",
			Parts:           []JumaMessagePart{{Type: "text", Text: "You are an expert image editing assistant. When the user provides an image, you MUST use the 'ImageEdit' tool to modify it according to their instructions. Do not just describe the edit. Always output the tool call."}},
			GeneratedImages: []any{},
			UploadedImages:  []any{},
			UploadedFiles:   []any{},
		}
		result = append(result, systemPrompt)
	}

	for _, msg := range msgs {
		role := msg.Get("role").String()
		if role == "system" && isNanobananaModel(model) {
			continue // Skip user-provided system prompts if we injected our own
		}

		contentRaw := msg.Get("content")

		var textContent string
		// Track images for THIS specific message only
		var msgImages []JumaUploadedImage

		// Handle both string content and array content
		if contentRaw.IsArray() {
			// OpenAI vision-style content array
			handleDataURLUpload := func(dataURL string) *JumaUploadedImage {
				log.Infof("juma executor: attempting Juma upload, sessionToken=%v, workspaceID=%v", sessionToken != "", workspaceID != "")

				if sessionToken == "" || workspaceID == "" {
					log.Warnf("juma executor: missing session token or workspace ID for image upload")
					return nil
				}

				uploadResult, err := UploadImageToJuma(sessionToken, workspaceID, dataURL)
				if err != nil {
					log.Warnf("juma executor: failed to upload image to Juma: %v", err)
					return nil
				}

				log.Infof("juma executor: uploaded image to Juma, ID: %s, KnowledgeItemID: %s, URL: %s", uploadResult.ID, uploadResult.KnowledgeItemID, uploadResult.ImageURL)

				if uploadResult.ID != "" && uploadResult.ImageURL != "" {
					img := JumaUploadedImage{
						ID:       uploadResult.ID,
						ImageURL: uploadResult.ImageURL,
						Name:     uploadResult.Name,
					}
					// Add to both message-specific and global lists
					msgImages = append(msgImages, img)
					uploadedImages = append(uploadedImages, img)
					log.Infof("juma executor: added image to uploadedImages: ID=%s, URL=%s", uploadResult.ID, uploadResult.ImageURL)
					return &img
				}
				log.Warnf("juma executor: no valid image ID or URL returned")
				return nil
			}

			for _, part := range contentRaw.Array() {
				partType := part.Get("type").String()
				if partType == "text" {
					textContent += part.Get("text").String()
				} else if partType == "image_url" || partType == "input_image" || partType == "image" {
					// Extract URL from various OpenAI-like vision formats.
					url := part.Get("image_url.url").String()
					if url == "" {
						url = part.Get("image_url").String()
					}
					if url == "" {
						url = part.Get("image.url").String()
					}
					if url == "" {
						url = part.Get("url").String()
					}
					if url != "" {
						log.Infof("juma executor: processing image URL, isDataURL=%v, cfgNil=%v", strings.HasPrefix(url, "data:"), cfg == nil)
						// Upload base64 images to Juma's native file storage
						if strings.HasPrefix(url, "data:") {
							handleDataURLUpload(url)
						} else if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
							dataURL, err := fetchImageDataURLFromHTTP(url, jumaMaxRemoteImageBytes)
							if err != nil {
								log.Warnf("juma executor: failed to fetch remote image for upload: %v", err)
							} else {
								handleDataURLUpload(dataURL)
							}
						} else {
							log.Warnf("juma executor: image URL not supported (must be data:, http, or https)")
						}
					}
				}
			}
		} else {
			textContent = contentRaw.String()
		}

		// Build parts - only text parts, images are passed via uploadedImages
		parts := []JumaMessagePart{}
		if textContent != "" {
			parts = append(parts, JumaMessagePart{Type: "text", Text: textContent})
		}

		// Build uploadedImages array for Juma's format
		// Images are NOT added to parts - Juma uses uploadedImages field instead
		msgUploadedImages := make([]any, 0)
		for _, img := range msgImages {
			msgUploadedImages = append(msgUploadedImages, map[string]any{
				"id":       img.ID,
				"imageUrl": img.ImageURL,
				"name":     img.Name,
			})
			log.Infof("juma executor: added image to uploadedImages: ID=%s, URL=%s", img.ID, img.ImageURL)
		}

		jumaMsg := JumaMessage{
			ID:              uuid.New().String(),
			Role:            role,
			Content:         textContent,
			Parts:           parts,
			GeneratedImages: []any{},
			UploadedImages:  msgUploadedImages,
			UploadedFiles:   []any{},
		}
		result = append(result, jumaMsg)
	}

	// Build knowledgeItems from uploaded images
	// Juma uses knowledgeItems to reference images in chat - this is the only way that works
	knowledgeItems := make([]map[string]string, 0, len(uploadedImages))
	for _, img := range uploadedImages {
		if img.ID != "" {
			knowledgeItems = append(knowledgeItems, map[string]string{
				"id":     img.ID,
				"source": "AttachedNewContextSnippet",
			})
			log.Infof("juma executor: added to knowledgeItems: ID=%s", img.ID)
		}
	}

	return JumaConversionResult{
		Messages:       result,
		KnowledgeItems: knowledgeItems,
		UploadedImages: uploadedImages,
	}
}

// fetchImageDataURLFromHTTP downloads a remote image and converts it to a data URL string.
// A size limit is enforced to avoid excessive memory usage.
func fetchImageDataURLFromHTTP(url string, maxBytes int64) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("image size exceeds limit (%d bytes)", maxBytes)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("content-type is not image: %s", contentType)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded), nil
}

func (e *JumaExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	sessionToken, workspaceID, vendorConnectionID := jumaCredentials(auth)
	if sessionToken == "" {
		err = statusErr{code: http.StatusUnauthorized, msg: "missing Juma session token"}
		return
	}

	// Find model by alias
	model := getJumaModelByAlias(req.Model)
	if model == nil {
		err = statusErr{code: http.StatusBadRequest, msg: fmt.Sprintf("unknown Juma model: %s", req.Model)}
		return
	}

	// Use model's vendor connection ID if not specified in config
	if vendorConnectionID == "" {
		vendorConnectionID = model.VendorConnectionID
	}

	// Build Juma request
	conversionResult := convertToJumaMessages(e.cfg, req.Payload, sessionToken, workspaceID)

	// Convert knowledge items to []any for JSON serialization
	knowledgeItems := make([]any, len(conversionResult.KnowledgeItems))
	for i, item := range conversionResult.KnowledgeItems {
		knowledgeItems[i] = item
	}

	jumaReq := JumaRequest{
		Messages:           conversionResult.Messages,
		ModelID:            model.ID,
		ThreadID:           uuid.New().String(),
		WorkspaceID:        workspaceID,
		VendorConnectionID: vendorConnectionID,
		IsNewThread:        true,
		PromptUsages:       []any{},
		KnowledgeItems:     knowledgeItems,
	}

	// Add ImageEdit tool for Nanobanana model
	if isNanobananaModel(req.Model) {
		jumaReq.Tools = []JumaTool{
			{
				Type: "function",
				Function: JumaToolFunction{
					Name:        "ImageEdit",
					Description: "Edit or generate images based on text prompts. Use this tool when the user asks to generate, edit, or modify images.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"prompt": map[string]any{
								"type":        "string",
								"description": "The prompt describing the image to generate or the edit to make",
							},
							"imageUrls": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "URLs of images to edit (optional for generation)",
							},
							"orientation": map[string]any{
								"type":        "string",
								"enum":        []string{"vertical", "horizontal", "square"},
								"description": "The orientation of the output image",
							},
						},
						"required": []string{"prompt"},
					},
				},
			},
		}
	}

	reqBody, err := json.Marshal(jumaReq)
	if err != nil {
		return resp, err
	}

	// Debug: log the request body to see what we're sending to Juma
	log.Infof("juma executor: sending request to Juma, knowledgeItems=%d, messages=%d", len(knowledgeItems), len(conversionResult.Messages))
	if len(conversionResult.Messages) > 0 {
		lastMsg := conversionResult.Messages[len(conversionResult.Messages)-1]
		log.Infof("juma executor: last message parts=%d, uploadedImages=%d", len(lastMsg.Parts), len(lastMsg.UploadedImages))
		for i, part := range lastMsg.Parts {
			log.Infof("juma executor: part[%d] type=%s, imageUrl=%s, imageId=%s", i, part.Type, part.ImageURL, part.ImageID)
		}
	}

	url := jumaBaseURL + "/api/chat/stream"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return resp, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Origin", jumaBaseURL)
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	httpReq.AddCookie(&http.Cookie{
		Name:  "__Secure-next-auth.session-token",
		Value: sessionToken,
	})

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      reqBody,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("juma executor: close response body error: %v", errClose)
		}
	}()
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		log.Errorf("juma executor: request error, status: %d, body: %s", httpResp.StatusCode, string(b))
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return resp, err
	}

	// For non-streaming, read all SSE data and extract the final content
	var fullContent strings.Builder
	var generatedImageURL string
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(nil, 20_971_520)

	for scanner.Scan() {
		line := scanner.Text()
		appendAPIResponseChunk(ctx, e.cfg, []byte(line))

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		// Parse events
		eventType := gjson.Get(data, "type").String()
		if eventType == "text-delta" {
			delta := gjson.Get(data, "delta").String()
			fullContent.WriteString(delta)
		} else if eventType == "tool-output-available" {
			// Extract generated image URL from tool output
			// Juma uses "ImageGeneration" or "ImageEdit" tools with output.imageUrl
			imageURL := gjson.Get(data, "output.imageUrl").String()
			if imageURL != "" {
				generatedImageURL = imageURL
			}
		}
	}

	// If we have an image but no text, or just to append the image
	if generatedImageURL != "" {
		// Append image markdown to content so it appears in Chat Completion
		if fullContent.Len() > 0 {
			fullContent.WriteString("\n\n")
		}
		fullContent.WriteString(fmt.Sprintf("![Generated Image](%s)", generatedImageURL))
	}

	if errScan := scanner.Err(); errScan != nil {
		recordAPIResponseError(ctx, e.cfg, errScan)
		return resp, errScan
	}

	reporter.ensurePublished(ctx)

	// Check if this is an image model and we have generated image URL
	if isNanobananaModel(req.Model) && generatedImageURL != "" {
		openAIResp := buildOpenAIImageResponse(generatedImageURL)
		resp = cliproxyexecutor.Response{Payload: openAIResp}
		return resp, nil
	}

	// Build OpenAI-style response
	openAIResp := buildOpenAIChatResponse(req.Model, fullContent.String())
	resp = cliproxyexecutor.Response{Payload: openAIResp}
	return resp, nil
}

func (e *JumaExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (stream <-chan cliproxyexecutor.StreamChunk, err error) {
	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	sessionToken, workspaceID, vendorConnectionID := jumaCredentials(auth)
	if sessionToken == "" {
		err = statusErr{code: http.StatusUnauthorized, msg: "missing Juma session token"}
		return nil, err
	}

	// Find model by alias
	model := getJumaModelByAlias(req.Model)
	if model == nil {
		err = statusErr{code: http.StatusBadRequest, msg: fmt.Sprintf("unknown Juma model: %s", req.Model)}
		return nil, err
	}

	// Use model's vendor connection ID if not specified in config
	if vendorConnectionID == "" {
		vendorConnectionID = model.VendorConnectionID
	}

	// Build Juma request
	conversionResult := convertToJumaMessages(e.cfg, req.Payload, sessionToken, workspaceID)

	// Convert knowledge items to []any for JSON serialization
	knowledgeItems := make([]any, len(conversionResult.KnowledgeItems))
	for i, item := range conversionResult.KnowledgeItems {
		knowledgeItems[i] = item
	}

	jumaReq := JumaRequest{
		Messages:           conversionResult.Messages,
		ModelID:            model.ID,
		ThreadID:           uuid.New().String(),
		WorkspaceID:        workspaceID,
		VendorConnectionID: vendorConnectionID,
		IsNewThread:        true,
		PromptUsages:       []any{},
		KnowledgeItems:     knowledgeItems,
	}

	// Add ImageEdit tool for Nanobanana model
	if isNanobananaModel(req.Model) {
		jumaReq.Tools = []JumaTool{
			{
				Type: "function",
				Function: JumaToolFunction{
					Name:        "ImageEdit",
					Description: "Edit or generate images based on text prompts. Use this tool when the user asks to generate, edit, or modify images.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"prompt": map[string]any{
								"type":        "string",
								"description": "The prompt describing the image to generate or the edit to make",
							},
							"imageUrls": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "URLs of images to edit (optional for generation)",
							},
							"orientation": map[string]any{
								"type":        "string",
								"enum":        []string{"vertical", "horizontal", "square"},
								"description": "The orientation of the output image",
							},
						},
						"required": []string{"prompt"},
					},
				},
			},
		}
	}

	reqBody, err := json.Marshal(jumaReq)
	if err != nil {
		return nil, err
	}

	// Debug: log the request body to see what we're sending to Juma
	log.Infof("juma executor stream: sending request to Juma, knowledgeItems=%d, messages=%d", len(knowledgeItems), len(conversionResult.Messages))
	if len(conversionResult.Messages) > 0 {
		lastMsg := conversionResult.Messages[len(conversionResult.Messages)-1]
		log.Infof("juma executor stream: last message parts=%d, uploadedImages=%d", len(lastMsg.Parts), len(lastMsg.UploadedImages))
		for i, part := range lastMsg.Parts {
			log.Infof("juma executor stream: part[%d] type=%s, imageUrl=%s, imageId=%s", i, part.Type, part.ImageURL, part.ImageID)
		}
	}

	url := jumaBaseURL + "/api/chat/stream"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Origin", jumaBaseURL)
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	httpReq.AddCookie(&http.Cookie{
		Name:  "__Secure-next-auth.session-token",
		Value: sessionToken,
	})

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      reqBody,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, b)
		log.Errorf("juma executor stream: request error, status: %d, body: %s", httpResp.StatusCode, string(b))
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("juma executor: close response body error: %v", errClose)
		}
		err = statusErr{code: httpResp.StatusCode, msg: string(b)}
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	stream = out

	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("juma executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 20_971_520)
		chunkIndex := 0

		for scanner.Scan() {
			line := scanner.Text()
			appendAPIResponseChunk(ctx, e.cfg, []byte(line))

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				// Stream complete, just break - handler will send [DONE] when channel closes
				break
			}

			// Parse Juma events and convert to OpenAI SSE format
			eventType := gjson.Get(data, "type").String()
			if eventType == "text-delta" {
				delta := gjson.Get(data, "delta").String()
				// Transform Juma's custom image tags to Markdown format
				transformedDelta := transformGeneratedImageTags(delta)
				chunk := buildOpenAIStreamChunk(req.Model, transformedDelta, chunkIndex)
				out <- cliproxyexecutor.StreamChunk{Payload: chunk}
				chunkIndex++
			} else if eventType == "tool-output-available" {
				// Juma uses "ImageGeneration" or "ImageEdit" tools with output.imageUrl
				imageURL := gjson.Get(data, "output.imageUrl").String()
				if imageURL != "" {
					chunk := buildOpenAIStreamChunk(req.Model, fmt.Sprintf("\n\n![Generated Image](%s)", imageURL), chunkIndex)
					out <- cliproxyexecutor.StreamChunk{Payload: chunk}
					chunkIndex++
				}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
		reporter.ensurePublished(ctx)
	}()

	return stream, nil
}

func (e *JumaExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	// Juma doesn't provide a token counting API, so we estimate
	return cliproxyexecutor.Response{}, fmt.Errorf("juma executor: token counting not supported")
}

// Refresh is a no-op for session token based authentication.
func (e *JumaExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("juma executor: refresh called (no-op)")
	return auth, nil
}

// buildOpenAIChatResponse builds an OpenAI-compatible chat completion response.
func buildOpenAIChatResponse(model, content string) []byte {
	// Transform Juma's custom image tags to Markdown format
	transformedContent := transformGeneratedImageTags(content)

	resp := map[string]any{
		"id":      "chatcmpl-" + uuid.New().String()[:8],
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": transformedContent,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// buildOpenAIStreamChunk builds an OpenAI-compatible SSE chunk.
// It returns only the JSON payload; the caller is responsible for adding SSE framing.
func buildOpenAIStreamChunk(model, delta string, index int) []byte {
	chunk := map[string]any{
		"id":      "chatcmpl-" + uuid.New().String()[:8],
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": index,
				"delta": map[string]any{
					"content": delta,
				},
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(chunk)
	return b
}

// isNanobananaModel checks if the given model alias is the Nanobanana Pro model.
func isNanobananaModel(modelAlias string) bool {
	return modelAlias == "juma-nanobanana-pro"
}

// transformGeneratedImageTags converts Juma's <generated-image> tags to standard Markdown image format.
// Converts: <generated-image url="..." /> or <generated-image url='...' />
// To: ![Generated Image](...)
func transformGeneratedImageTags(content string) string {
	// Match both single and double quoted URLs
	// Pattern: <generated-image url="..." /> or <generated-image url='...' />
	re := regexp.MustCompile(`<generated-image\s+url=["']([^"']+)["']\s*/?>`)
	return re.ReplaceAllString(content, "![Generated Image]($1)")
}

// buildOpenAIImageResponse builds an OpenAI-compatible image generation response.
func buildOpenAIImageResponse(imageURL string) []byte {
	resp := map[string]any{
		"created": time.Now().Unix(),
		"data": []map[string]any{
			{
				"url": imageURL,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}
