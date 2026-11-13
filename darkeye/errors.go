package darkeye

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"

	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

// requestError describes a failed API request. Its message never contains a
// response body, a request URL or an API key.
type requestError struct {
	product   string
	status    int // HTTP status, 0 when there was no response
	quota     bool
	transient bool
	msg       string
}

func (e *requestError) Error() string { return e.product + ": " + e.msg }

// Retryable reports whether repeating the same request may succeed.
func (e *requestError) Retryable() bool { return e.transient }

// Quota reports that the product's daily request budget is used up (HTTP 429).
func (e *requestError) Quota() bool { return e.quota }

func statusError(product string, status int) *requestError {
	switch {
	case status == 429:
		return &requestError{product: product, status: status, quota: true,
			msg: "daily request budget is used up (HTTP 429); try again after the daily reset"}
	case status == 401 || status == 403:
		return &requestError{product: product, status: status,
			msg: fmt.Sprintf("API returned HTTP %d; check the API key and that the plan includes API access", status)}
	case status >= 500:
		return &requestError{product: product, status: status, transient: true,
			msg: fmt.Sprintf("API returned HTTP %d", status)}
	}
	return &requestError{product: product, status: status, msg: fmt.Sprintf("API returned HTTP %d", status)}
}

func permanentError(product, msg string) *requestError {
	return &requestError{product: product, msg: msg}
}

// networkError classifies a transport failure. Context cancellation is returned
// as is so it is never retried.
func networkError(ctx context.Context, product string, err error, what string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if isTransientNetworkError(err) {
		return &requestError{product: product, transient: true, msg: what + " (network error)"}
	}
	return &requestError{product: product, msg: what}
}

// isTransientNetworkError is true for timeouts, resets, refused or dropped
// connections and temporary DNS failures. TLS, URL and permanent DNS failures
// are not transient.
func isTransientNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsTimeout || dns.IsTemporary
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// noRetryError marks an error as not retryable. Once rows were streamed to
// Steampipe, retrying the whole list call would repeat them.
type noRetryError struct{ error }

func (e noRetryError) Unwrap() error   { return e.error }
func (e noRetryError) Retryable() bool { return false }

// shouldRetryError is the plugin's ShouldRetryErrorFunc: 5xx responses and
// transient network errors are retried; 429, other 4xx and everything else is not.
func shouldRetryError(_ context.Context, _ *plugin.QueryData, _ *plugin.HydrateData, err error) bool {
	var r interface{ Retryable() bool }
	return errors.As(err, &r) && r.Retryable()
}
