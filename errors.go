package toxpt

import (
	"fmt"

	torerrors "github.com/opd-ai/go-tor/pkg/errors"
)

var (
	ErrInvalidConfig = torerrors.New(torerrors.CategoryConfiguration, torerrors.SeverityHigh, "invalid toxpt configuration")
	ErrUnauthorized  = torerrors.New(torerrors.CategoryProtocol, torerrors.SeverityHigh, "unauthorized tox peer")
	ErrNotRunning    = torerrors.New(torerrors.CategoryNetwork, torerrors.SeverityMedium, "transport not running")
)

func wrapNetwork(message string, err error) error {
	if err == nil {
		return torerrors.New(torerrors.CategoryNetwork, torerrors.SeverityMedium, message)
	}
	return torerrors.Wrap(torerrors.CategoryNetwork, torerrors.SeverityMedium, message, err)
}

func wrapProtocol(message string, err error) error {
	if err == nil {
		return torerrors.New(torerrors.CategoryProtocol, torerrors.SeverityHigh, message)
	}
	return torerrors.Wrap(torerrors.CategoryProtocol, torerrors.SeverityHigh, message, err)
}

func wrapConfig(message string, err error) error {
	if err == nil {
		return torerrors.New(torerrors.CategoryConfiguration, torerrors.SeverityHigh, message)
	}
	return torerrors.Wrap(torerrors.CategoryConfiguration, torerrors.SeverityHigh, message, err)
}

func joinErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
