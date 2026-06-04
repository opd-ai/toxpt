package toxpt

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/opd-ai/toxcore"
)

const defaultListenPort uint16 = 33445

// Config configures Tox pluggable transport and bridge behavior.
type Config struct {
	// ToxClient is the existing Tox instance to use for transport.
	// This allows toxpt to be embedded in a user-facing Tox client.
	ToxClient *toxcore.Tox

	// AllowedFriends specifies which Tox public keys can use the bridge.
	// If nil or empty, the bridge will dynamically allow all friends from ToxClient.
	AllowedFriends [][32]byte

	BridgeORPort    uint16
	// ClientPublicKey is the Tox public key used to identify this client
	// when dialing through the transport. Required for client-mode operation.
	ClientPublicKey [32]byte
	Logger          *slog.Logger
}

// DefaultConfig returns a safe baseline configuration.
func DefaultConfig() Config {
	return Config{
		BridgeORPort: 9001,
		Logger:       slog.Default(),
	}
}

// Validate validates the transport configuration.
func (cfg Config) Validate() error {
	if cfg.ToxClient == nil {
		return fmt.Errorf("tox client is required: %w", ErrInvalidConfig)
	}
	if cfg.BridgeORPort == 0 {
		return fmt.Errorf("bridge OR port must be non-zero: %w", ErrInvalidConfig)
	}
	// AllowedFriends can be empty - we'll use the client's friend list dynamically
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
