package pelican

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type HttpClient struct {
	Token  string
	URL    string
	client *http.Client
}

func NewHttpClient(token, url string) *HttpClient {
	return &HttpClient{
		Token:  token,
		URL:    url,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// buildUrl combines the base URL with the provided endpoint path.
func (c *HttpClient) buildUrl(endpoint string) (string, error) {
	base, err := url.Parse(c.URL)
	if err != nil {
		return "", err
	}
	if base.Scheme != "https" && base.Scheme != "http" || base.Host == "" {
		return "", fmt.Errorf("invalid Pelican URL %q", c.URL)
	}
	joined, err := url.JoinPath(base.String(), "api", "client", endpoint)
	return joined, err
}

// Get sends a GET request to the specified endpoint and returns the response body.
func (c *HttpClient) Get(endpoint string) ([]byte, error) {
	fullURL, err := c.buildUrl(endpoint)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned status %d", fullURL, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Post sends a POST request with a JSON body to the specified endpoint and returns the response body.
func (c *HttpClient) Post(endpoint string, body interface{}) ([]byte, error) {
	fullURL, err := c.buildUrl(endpoint)
	if err != nil {
		return nil, err
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fullURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s returned status %d", fullURL, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (c *HttpClient) StartServer(server string) error {
	endpoint := fmt.Sprintf("servers/%s/power", server)
	body := map[string]interface{}{
		"signal": "start",
	}
	_, err := c.Post(endpoint, body)
	if err != nil {
		return fmt.Errorf("error starting server %s: %w", server, err)
	}
	return nil
}

func (c *HttpClient) StopServer(server string) error {
	endpoint := fmt.Sprintf("servers/%s/power", server)
	body := map[string]interface{}{
		"signal": "stop",
	}
	_, err := c.Post(endpoint, body)
	if err != nil {
		return fmt.Errorf("error stopping server %s: %w", server, err)
	}
	return nil
}
