package toxpt

import (
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

	BridgeORPort      uint16
	InboundBufferSize int
	// ClientPublicKey is the Tox public key used to identify this client
	// when dialing through the transport.
	ClientPublicKey [32]byte
	Logger          *slog.Logger
}

// DefaultConfig returns a safe baseline configuration.
func DefaultConfig() Config {
	return Config{
		BridgeORPort:      9001,
		InboundBufferSize: 16,
		Logger:            slog.Default(),
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
	if err := validateInboundBufferSize(cfg.InboundBufferSize); err != nil {
		return err
	}
	if err := validateAllowedFriends(cfg.AllowedFriends); err != nil {
		return err
	}
	if err := validateLogger(cfg.Logger); err != nil {
		return err
	}
	return nil
}

func validateInboundBufferSize(size int) error {
	if size < 0 {
		return fmt.Errorf("inbound buffer size must be non-negative: %w", ErrInvalidConfig)
	}
	return nil
}

func validateAllowedFriends(allowed [][32]byte) error {
	// AllowedFriends can be empty - we'll use the client's friend list dynamically
	for i, pk := range allowed {
		if pk == ([32]byte{}) {
			return fmt.Errorf("allowed friend at index %d is zero key: %w", i, ErrInvalidConfig)
		}
	}
	return nil
}

func validateLogger(logger *slog.Logger) error {
	if logger == nil {
		return fmt.Errorf("logger must be non-nil: %w", ErrInvalidConfig)
	}
	return nil
}
