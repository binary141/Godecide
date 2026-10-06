package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DefaultBaseURL matches the Godecide server's default listen address.
const DefaultBaseURL = "http://localhost:8080"

// Client is a Godecide HTTP API client. The zero value is not usable; construct
// one with New. A Client is safe for concurrent use.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client constructed by New.
type Option func(*Client)

// WithHTTPClient overrides the http.Client used for requests, e.g. to set a
// custom timeout, transport, or add authentication via a RoundTripper.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New creates a Client for the Godecide server at baseURL (e.g.
// "http://localhost:8080"). Trailing slashes in baseURL are ignored.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is returned when the server responds with a non-2xx status. It
// carries the parsed error body when the server returned one.
type APIError struct {
	StatusCode int
	Message    string
	Problems   []string
}

func (e *APIError) Error() string {
	if len(e.Problems) > 0 {
		return fmt.Sprintf("godecide: %s (status %d): %s", e.Message, e.StatusCode, strings.Join(e.Problems, "; "))
	}
	return fmt.Sprintf("godecide: %s (status %d)", e.Message, e.StatusCode)
}

// errorBody mirrors the ad-hoc gin.H{"error": ..., "problems": ...} shape
// used across the server's handlers.
type errorBody struct {
	Error    string   `json:"error"`
	Problems []string `json:"problems,omitempty"`
}

// request sends an HTTP request and returns the raw status code and body.
// It only fails for transport-level problems (building the request, DNS,
// connection, reading the body) - a non-2xx status is not itself an error,
// since callers like the evaluate endpoints need to distinguish "request
// rejected" from "request accepted but evaluation failed" by status code.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("godecide: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("godecide: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("godecide: read response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

// do sends an HTTP request and decodes a JSON response into out (skipped if
// out is nil, e.g. for 204 No Content). It returns an *APIError for any
// non-2xx response.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, out any) error {
	status, respBody, err := c.request(ctx, method, path, query, body)
	if err != nil {
		return err
	}

	if status < 200 || status >= 300 {
		return apiErrorFromBody(status, respBody)
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("godecide: decode response: %w", err)
	}
	return nil
}

// apiErrorFromBody builds an *APIError from a non-2xx status and its
// (possibly JSON) body, falling back to the status text when the body
// isn't the server's usual {"error": ...} shape.
func apiErrorFromBody(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status, Message: http.StatusText(status)}
	var eb errorBody
	if json.Unmarshal(body, &eb) == nil && eb.Error != "" {
		apiErr.Message = eb.Error
		apiErr.Problems = eb.Problems
	}
	return apiErr
}

func paginationQuery(opts ListOptions) url.Values {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	return q
}

// Healthcheck calls GET /healthz to verify the server is reachable and
// responding.
func (c *Client) Healthcheck(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil, nil)
}
