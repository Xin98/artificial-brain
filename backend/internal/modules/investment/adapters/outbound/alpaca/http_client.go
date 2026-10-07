package alpaca

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type ProviderError struct {
	Code      string
	Retryable bool
}

func (e *ProviderError) Error() string { return e.Code }

type readClient struct {
	client      *http.Client
	key, secret string
	timeout     time.Duration
}

func newReadClient(client *http.Client, key, secret string, timeout time.Duration) *readClient {
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &readClient{&copyClient, key, secret, timeout}
}
func validBase(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}
func (c *readClient) get(ctx context.Context, base, path string, query url.Values, target any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	u, e := url.Parse(base)
	if e != nil {
		return &ProviderError{Code: "provider_configuration_invalid"}
	}
	u.Path = path
	u.RawQuery = query.Encode()
	for attempt := 0; attempt < 3; attempt++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			return e
		}
		req.Header.Set("APCA-API-KEY-ID", c.key)
		req.Header.Set("APCA-API-SECRET-KEY", c.secret)
		req.Header.Set("Accept", "application/json")
		response, e := c.client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return &ProviderError{Code: "provider_timeout", Retryable: true}
			}
			if attempt < 2 {
				if e = wait(ctx, time.Duration(attempt+1)*100*time.Millisecond); e != nil {
					return e
				}
				continue
			}
			return &ProviderError{Code: "provider_unavailable", Retryable: true}
		}
		status := response.StatusCode
		if status == http.StatusOK {
			decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
			decoder.UseNumber()
			e = decoder.Decode(target)
			response.Body.Close()
			if e != nil {
				return &ProviderError{Code: "provider_response_invalid"}
			}
			return nil
		}
		response.Body.Close()
		code := "provider_request_failed"
		retry := status == 429 || status >= 500
		if status == 401 {
			code = "provider_authentication_failed"
		}
		if status == 403 {
			code = "provider_entitlement_denied"
		}
		if status == 429 {
			code = "provider_rate_limited"
		}
		if status >= 500 {
			code = "provider_unavailable"
		}
		if !retry || attempt == 2 {
			return &ProviderError{Code: code, Retryable: retry}
		}
		delay := time.Duration(attempt+1) * 100 * time.Millisecond
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds >= 0 {
			if seconds > 2 {
				seconds = 2
			}
			delay = time.Duration(seconds) * time.Second
		}
		if e = wait(ctx, delay); e != nil {
			return &ProviderError{Code: "provider_timeout", Retryable: true}
		}
	}
	return &ProviderError{Code: "provider_unavailable", Retryable: true}
}
func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
