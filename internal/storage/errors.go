package storage

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// wrapAzure turns an SDK error into something a TUI status bar can show
// in one line. The raw azcore.ResponseError prints a full HTTP dump,
// which scrolls a modal off the screen and buries the actual cause.
func wrapAzure(op string, err error) error {
	if err == nil {
		return nil
	}

	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		code := respErr.ErrorCode
		if code == "" {
			code = respErr.RawResponse.Status
		}
		// The single most common local failure: the Go SDK sends a newer
		// x-ms-version than the running Azurite accepts, and the only
		// clue is an opaque 400. Name it so the fix is obvious.
		if code == "InvalidHeaderValue" || code == "UnsupportedHttpVerb" {
			return fmt.Errorf("%s: %s — Azurite may be older than this SDK's API version; "+
				"enable azurite.skipApiVersionCheck or upgrade Azurite", op, code)
		}
		return fmt.Errorf("%s: %s (%d)", op, code, respErr.StatusCode)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s: timed out — is Azurite running?", op)
	}
	if isConnRefused(err) {
		return fmt.Errorf("%s: connection refused — Azurite is not listening", op)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func isConnRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return strings.Contains(err.Error(), "connection refused")
}

// isAlreadyExists lets create operations be idempotent, which is what
// makes `azstore seed apply` safe to re-run.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.ErrorCode {
		case "ContainerAlreadyExists", "QueueAlreadyExists", "TableAlreadyExists",
			"ResourceAlreadyExists", "EntityAlreadyExists":
			return true
		}
		return respErr.StatusCode == 409
	}
	return false
}

// IsNotFound reports whether an error is a 404, so callers can treat a
// missing resource as an empty result rather than a failure.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == 404
	}
	return false
}
