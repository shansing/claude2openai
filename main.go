/*
 * @Author: Vincent Yang
 * @Date: 2024-03-18 01:12:14
 * @LastEditors: Vincent Yang
 * @LastEditTime: 2024-03-30 02:06:41
 * @FilePath: /claude2openai/main.go
 * @Telegram: https://t.me/missuo
 * @GitHub: https://github.com/missuo
 *
 * Copyright © 2024 by Vincent, All Rights Reserved.
 */

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const baseUrl = "http://localhost:1201" //"https://api.anthropic.com"
const listen = ":1204"
const shansingAuthorizationHeader = true
const shansingOnlineSearch = true

func processMessages(openAIReq OpenAIRequest) []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
} {
	var newMessages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	for i := 0; i < len(openAIReq.Messages); i++ {
		if openAIReq.Messages[i].Role == "system" && i+1 < len(openAIReq.Messages) {
			openAIReq.Messages[i+1].Content = openAIReq.Messages[i].Content + " " + openAIReq.Messages[i+1].Content
		} else if openAIReq.Messages[i].Role != "system" {
			newMessages = append(newMessages, openAIReq.Messages[i])
		}
	}
	return newMessages
}

func createClaudeRequest(openAIReq OpenAIRequest, stream bool) ([]byte, error) {
	var maxTokens = openAIReq.MaxTokens
	if maxTokens <= 0 || maxTokens > 4096 {
		maxTokens = 4096
	}
	return json.Marshal(map[string]interface{}{
		"model":      openAIReq.Model,
		"max_tokens": maxTokens,
		"messages":   openAIReq.Messages,
		"stream":     stream,
	})
}

func parseAuthorizationHeader(c *gin.Context) (string, error) {
	authorizationHeader := c.GetHeader("Authorization")
	if !strings.HasPrefix(authorizationHeader, "Bearer ") {
		return "", fmt.Errorf("invalid Authorization header format")
	}
	return strings.TrimPrefix(authorizationHeader, "Bearer "), nil
}

func sendClaudeRequest(claudeReqBody []byte, apiKey string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", baseUrl+"/v1/messages", bytes.NewBuffer(claudeReqBody))
	req.Header.Set("Content-Type", "application/json")
	if shansingAuthorizationHeader {
		req.Header.Set("Authorization", "Basic "+apiKey)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}
	if shansingOnlineSearch {
		req.Header.Set("X-Shansing-Online-Search", "true")
	}
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	return client.Do(req)
}

func proxyToClaude(c *gin.Context, openAIReq OpenAIRequest) {
	openAIReq.Messages = processMessages(openAIReq)

	claudeReqBody, err := createClaudeRequest(openAIReq, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request for Claude API"})
		return
	}

	apiKey, err := parseAuthorizationHeader(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := sendClaudeRequest(claudeReqBody, apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to call Claude API"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response from Claude API"})
		return
	}

	var claudeResp ClaudeAPIResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse response from Claude API"})
		return
	}

	openAIResp := OpenAIResponse{
		ID:      claudeResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   claudeResp.Model,
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			Logprobs     interface{} `json:"logprobs"`
			FinishReason string      `json:"finish_reason"`
		}{
			{
				Index: 0,
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{
					Role:    "assistant",
					Content: claudeResp.Content[0].Text,
				},
				Logprobs:     nil,
				FinishReason: "stop",
			},
		},
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     len(openAIReq.Messages[0].Content),
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
	}
	c.JSON(http.StatusOK, openAIResp)
}

func proxyToClaudeStream(c *gin.Context, openAIReq OpenAIRequest) {
	openAIReq.Messages = processMessages(openAIReq)

	claudeReqBody, err := createClaudeRequest(openAIReq, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal request for Claude API"})
		return
	}

	apiKey, err := parseAuthorizationHeader(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := sendClaudeRequest(claudeReqBody, apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send request to Claude API"})
		return
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	var content string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response from Claude API"})
			return
		}

		lineStr := strings.TrimSpace(string(line))
		if strings.HasPrefix(lineStr, "event: message_start") {
			c.SSEvent("message", fmt.Sprintf(`{"id":"chatcmpl-%s","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
				uuid.New().String(), time.Now().Unix(), openAIReq.Model))
			flusher.Flush()
		} else if strings.HasPrefix(lineStr, "data:") {
			dataStr := strings.TrimSpace(strings.TrimPrefix(lineStr, "data:"))
			var data map[string]interface{}
			json.Unmarshal([]byte(dataStr), &data)
			if data["type"] == "content_block_delta" {
				delta := data["delta"].(map[string]interface{})
				if delta["type"] == "text_delta" {
					content += delta["text"].(string)
					c.SSEvent("message", fmt.Sprintf(`{"id":"chatcmpl-%s","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}]}`,
						uuid.New().String(), time.Now().Unix(), openAIReq.Model, escapeJSON(delta["text"].(string))))
					flusher.Flush()
				}
			}
		} else if strings.HasPrefix(lineStr, "event: message_stop") {
			c.SSEvent("message", fmt.Sprintf(`{"id":"chatcmpl-%s","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				uuid.New().String(), time.Now().Unix(), openAIReq.Model))
			flusher.Flush()
			break
		}
	}
}

func escapeJSON(str string) string {
	b, _ := json.Marshal(str)
	return string(b[1 : len(b)-1])
}

func handler(c *gin.Context) {
	var openAIReq OpenAIRequest

	if err := c.BindJSON(&openAIReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	allowModels := []string{"claude-3-haiku-20240307", "claude-3-sonnet-20240229", "claude-3-opus-20240229"}

	// Default model is claude-3-haiku-20240307
	if !isInSlice(openAIReq.Model, allowModels) {
		openAIReq.Model = "claude-3-haiku-20240307"
	}

	// If stream is true, proxy to Claude with stream
	if openAIReq.Stream {
		proxyToClaudeStream(c, openAIReq)
	} else {
		proxyToClaude(c, openAIReq)
	}
}

func isInSlice(str string, list []string) bool {
	for _, item := range list {
		if item == str {
			return true
		}
	}
	return false
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.Use(cors.Default())
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to Claude2OpenAI, Made by Vincent Yang. https://github.com/missuo/claude2openai",
		})
	})
	r.POST("/v1/chat/completions", handler)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "Path not found",
		})
	})
	r.Run(listen)
}
