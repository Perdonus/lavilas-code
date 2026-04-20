package provider

import (
	"context"
	"fmt"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

type Client interface {
	Name() string
	Capabilities() Capabilities
	Create(context.Context, runtime.Request) (*runtime.Response, error)
	Stream(context.Context, runtime.Request) (runtime.Stream, error)
}

type Capabilities struct {
	Streaming bool
	Tools     bool
	Images    bool
	Audio     bool
}

type Error struct {
	Provider   string
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
}

func (e *Error) Error() string {
	providerName := e.Provider
	if providerName == "" {
		providerName = "provider"
	}
	if e.StatusCode > 0 {
		if e.Code != "" {
			return fmt.Sprintf("%s error (%d/%s): %s", providerName, e.StatusCode, e.Code, e.Message)
		}
		return fmt.Sprintf("%s error (%d): %s", providerName, e.StatusCode, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s error (%s): %s", providerName, e.Code, e.Message)
	}
	return fmt.Sprintf("%s error: %s", providerName, e.Message)
}
