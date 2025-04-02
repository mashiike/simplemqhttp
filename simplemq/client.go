package simplemq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Client struct {
	Endpoint   string
	APIKey     string
	Queue      string
	HTTPClient *http.Client
}

func NewClient(apiKey, queue string) *Client {
	return &Client{
		APIKey: apiKey,
		Queue:  queue,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Code, e.Message)
}

// doRequest handles common HTTP request operations
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url, err := c.endpointURL(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// SendMessage sends a message to the queue.
func (c *Client) SendMessage(ctx context.Context, content string) (*Message, error) {
	message := map[string]string{"content": content}
	body, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/v1/queues/"+c.Queue+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if err := dec.Decode(&apiErr); err != nil {
			return nil, fmt.Errorf("decode error: %w", err)
		}
		return nil, &apiErr
	}
	var result struct {
		Message Message `json:"message"`
	}
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	return &result.Message, nil
}

// ReceiveMessage receives a single message from the queue.
func (c *Client) ReceiveMessages(ctx context.Context) ([]Message, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/queues/"+c.Queue+"/messages", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if err := dec.Decode(&apiErr); err != nil {
			return nil, fmt.Errorf("decode error: %w", err)
		}
		return nil, &apiErr
	}

	var result struct {
		Messages []Message `json:"messages"`
	}

	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	if len(result.Messages) == 0 {
		return []Message{}, nil
	}
	return result.Messages, nil
}

// DeleteMessage deletes (acknowledges) a message from the queue.
func (c *Client) DeleteMessage(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/v1/queues/"+c.Queue+"/messages/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			return fmt.Errorf("decode error: %w", err)
		}
		return &apiErr
	}

	return nil
}

func (c *Client) ExtendVisibilityTimeout(ctx context.Context, id string) (*Message, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, "/v1/queues/"+c.Queue+"/messages/"+id, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if err := dec.Decode(&apiErr); err != nil {
			return nil, fmt.Errorf("decode error: %w", err)
		}
		return nil, &apiErr
	}
	var result struct {
		Message Message `json:"message"`
	}
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	return &result.Message, nil
}

const DefaultEndpoint = "https://simplemq.tk1b.api.sacloud.jp"

// endpointURL joins base endpoint with a path.
func (c *Client) endpointURL(p string) (string, error) {
	e := c.Endpoint
	if e == "" {
		e = DefaultEndpoint
	}
	u, err := url.Parse(e)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint URL: %w", err)
	}

	return u.JoinPath(p).String(), nil
}
