package toxpt

import (
	"errors"
	"fmt"
	"log/slog"
)

const defaultListenPort uint16 = 33445

// Config configures Tox pluggable transport and bridge behavior.
type Config struct {
	ToxSecretKey    [32]byte
	AllowedFriends  [][32]byte
	ListenPort      uint16
	BridgeORPort    uint16
	ClientPublicKey [32]byte
	Logger          *slog.Logger
}

// DefaultConfig returns a safe baseline configuration.
func DefaultConfig() Config {
	return Config{
		ListenPort:   defaultListenPort,
		BridgeORPort: 9001,
		Logger:       slog.Default(),
	}
}

// Validate validates the transport configuration.
func (cfg Config) Validate() error {
	if cfg.ToxSecretKey == ([32]byte{}) {
		return fmt.Errorf("tox secret key is required: %w", ErrInvalidConfig)
	}
	if len(cfg.AllowedFriends) == 0 {
		return fmt.Errorf("at least one allowed friend is required: %w", ErrInvalidConfig)
	}
	if cfg.ListenPort == 0 {
		return fmt.Errorf("listen port must be non-zero: %w", ErrInvalidConfig)
	}
	if cfg.BridgeORPort == 0 {
		return fmt.Errorf("bridge OR port must be non-zero: %w", ErrInvalidConfig)
	}
	for i, pk := range cfg.AllowedFriends {
		if pk == ([32]byte{}) {
			return fmt.Errorf("allowed friend at index %d is zero key: %w", i, ErrInvalidConfig)
		}
	}
	if cfg.Logger == nil {
		return errors.New("logger must be non-nil")
	}
	return nil
}
