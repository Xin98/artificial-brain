package openai

import "time"

// Config carries everything the OpenAI-compatible adapter needs. CI and
// compose never point it at a real model (global constraint 10).
type Config struct {
	BaseURL   string
	APIKey    string
	ModelName string
	Timeout   time.Duration
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
