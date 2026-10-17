package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

const DefaultBaseURL = "https://api.cuentica.com"

const (
	maxResponseBytes   = int64(64 << 20)
	maxErrorCharacters = 4096
)

type Client struct {
	baseURL       *url.URL
	token         string
	secretPattern *regexp.Regexp
	http          *http.Client
	responseLimit int64
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

type Error struct {
	Status     int
	Body       string
	RetryAfter string
}

func (e *Error) Error() string {
	if status := http.StatusText(e.Status); status != "" {
		return fmt.Sprintf("API returned HTTP %d %s", e.Status, status)
	}
	return fmt.Sprintf("API returned HTTP %d", e.Status)
}

func New(baseURL, token string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		return nil, errors.New("request timeout must be greater than zero")
	}
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if strings.ContainsFunc(token, unicode.IsControl) {
		return nil, errors.New("API key contains invalid control characters")
	}
	redactor, err := secretPattern(token)
	if err != nil {
		return nil, errors.New("API key cannot be encoded safely")
	}
	return &Client{
		baseURL:       base,
		token:         token,
		secretPattern: redactor,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		responseLimit: maxResponseBytes,
	}, nil
}

func (c *Client) SetTransport(transport http.RoundTripper) {
	c.http.Transport = transport
}

func parseBaseURL(value string) (*url.URL, error) {
	base, err := url.Parse(value)
	if err != nil || base.Host == "" || base.Hostname() == "" || base.User != nil || strings.ContainsAny(value, "?#") || base.Opaque != "" || (base.EscapedPath() != "" && base.EscapedPath() != "/") {
		return nil, errors.New("base URL must be an HTTPS origin without credentials, a path, query, or fragment")
	}
	if strings.HasSuffix(base.Host, ":") {
		return nil, errors.New("base URL has an invalid port")
	}
	if port := base.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("base URL has an invalid port")
		}
	}
	if base.Scheme != "https" {
		ip := net.ParseIP(base.Hostname())
		if base.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("base URL must use HTTPS; HTTP is allowed only for literal loopback addresses")
		}
	}
	base.Path = ""
	return base, nil
}

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) (*Response, error) {
	endpoint, err := c.endpoint(path, query)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, c.requestError("cannot create API request", err)
	}
	req.Header.Set("X-AUTH-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cuentica-cli")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	result, err := c.http.Do(req)
	if err != nil {
		return nil, c.requestError("API request failed", err)
	}
	defer result.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(result.Body, c.responseLimit+1))
	if err != nil {
		return nil, c.requestError("cannot read API response", err)
	}
	if int64(len(payload)) > c.responseLimit {
		return nil, fmt.Errorf("API response exceeds the %d-byte limit", c.responseLimit)
	}
	response := &Response{Status: result.StatusCode, Header: result.Header.Clone(), Body: payload}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		errorBody := c.sanitize(string(payload), maxErrorCharacters)
		response.Body = []byte(errorBody)
		return response, &Error{
			Status:     result.StatusCode,
			Body:       errorBody,
			RetryAfter: c.sanitize(result.Header.Get("Retry-After"), maxErrorCharacters),
		}
	}
	return response, nil
}

func (c *Client) endpoint(path string, query url.Values) (*url.URL, error) {
	relative, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || relative.IsAbs() || relative.Host != "" || relative.User != nil || strings.ContainsAny(path, "?#\\") || strings.ContainsFunc(path, unicode.IsControl) || relative.Opaque != "" {
		return nil, errors.New("API path must be an absolute path without a host, query, or fragment")
	}
	// Decode repeatedly so encoded separators or dots cannot bypass path checks.
	decoded := relative.Path
	for {
		if strings.Contains(decoded, "\\") || strings.ContainsFunc(decoded, unicode.IsControl) || strings.HasPrefix(decoded, "//") {
			return nil, errors.New("API path contains an unsafe separator")
		}
		for _, segment := range strings.Split(decoded, "/") {
			if segment == "." || segment == ".." {
				return nil, errors.New("API path cannot contain traversal segments")
			}
		}
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = next
	}
	endpoint := *c.baseURL
	endpoint.Path = relative.Path
	endpoint.RawPath = relative.RawPath
	endpoint.RawQuery = query.Encode()
	return &endpoint, nil
}

func (c *Client) sanitize(value string, limit int) string {
	if c.token != "" {
		value = strings.ReplaceAll(value, c.token, "[REDACTED]")
		value = c.secretPattern.ReplaceAllString(value, "[REDACTED]")
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	characters := []rune(value)
	if len(characters) > limit {
		return string(characters[:limit])
	}
	return value
}

func secretPattern(token string) (*regexp.Regexp, error) {
	var pattern strings.Builder
	for _, character := range token {
		alternatives := []string{regexp.QuoteMeta(string(character))}
		encoded, _ := json.Marshal(string(character))
		alternatives = append(alternatives, regexp.QuoteMeta(string(encoded[1:len(encoded)-1])))
		if character == '/' {
			alternatives = append(alternatives, regexp.QuoteMeta(`\/`))
		}
		var escaped strings.Builder
		for _, unit := range utf16.Encode([]rune{character}) {
			escaped.WriteString(fmt.Sprintf(`\\u(?i:%04x)`, unit))
		}
		alternatives = append(alternatives, escaped.String())
		pattern.WriteString("(?:" + strings.Join(alternatives, "|") + ")")
	}
	return regexp.Compile(pattern.String())
}

type requestError struct {
	message string
	cause   error
}

func (e *requestError) Error() string { return e.message }
func (e *requestError) Unwrap() error { return e.cause }

func (c *Client) requestError(message string, cause error) error {
	return &requestError{message: message + ": " + c.sanitize(cause.Error(), maxErrorCharacters), cause: cause}
}
