# toxpt

`github.com/opd-ai/toxpt` is a pure-Go Tor pluggable transport that wraps Tor cell payloads in framed Tox messages and enforces friend-only bridge access.

## Design Philosophy

toxpt is designed to be embedded in user-facing Tox clients, allowing Tox users to offer Tor bridges to their friends over the Tox P2P network. Instead of creating its own Tox instance, toxpt accepts an existing `*toxcore.Tox` client, enabling seamless integration with Tox applications.

## Security model

- Friend ACL checks happen before inbound cell payloads are accepted.
- Public key ACL comparisons use constant-time comparison (`crypto/subtle.ConstantTimeCompare`).
- The bridge uses the existing Tox client's security configuration (typically Noise-IK encryption over TCP).
- Framing uses a fixed 4-byte big-endian length prefix with payload limits.
- If no explicit friend list is provided, the bridge dynamically allows all friends from the Tox client.

## Usage

### Embedding in a Tox Client

```go
import (
    "context"
    "github.com/opd-ai/toxcore"
    "github.com/opd-ai/toxpt"
)

// Create or use your existing Tox client
opts := toxcore.NewOptions()
opts.UDPEnabled = false  // Recommended for Tor bridge use
opts.LocalDiscovery = false
toxClient, err := toxcore.New(opts)
if err != nil {
    panic(err)
}
defer toxClient.Kill()

// Bootstrap to the Tox network
if err := toxClient.BootstrapDefaults(); err != nil {
    panic(err)
}

// Create the toxpt bridge using your existing Tox client
cfg := toxpt.DefaultConfig()
cfg.ToxClient = toxClient
// Optional: specify allowed friends explicitly
// cfg.AllowedFriends = [][32]byte{friendPublicKey}
// If not specified, all friends are allowed

bridge, err := toxpt.NewEmbeddableBridge(cfg)
if err != nil {
    panic(err)
}

ctx := context.Background()
if err := bridge.Start(ctx); err != nil {
    panic(err)
}
defer bridge.Stop()

// Your Tox client continues to function normally
// while also serving as a Tor bridge to your friends
```

### Friend Management

The bridge can operate in two modes:

1. **Dynamic friend list** (default): If `AllowedFriends` is empty or nil, the bridge automatically allows all current friends from the Tox client.

2. **Explicit friend list**: Specify `AllowedFriends` to restrict access to specific Tox public keys:

```go
cfg := toxpt.DefaultConfig()
cfg.ToxClient = myToxClient
cfg.AllowedFriends = [][32]byte{
    friendPublicKey1,
    friendPublicKey2,
}
```

## Benefits of Tox-based Bridges

- **Friend-only access**: Bridge access is automatically restricted to your Tox friends
- **Decentralized discovery**: No centralized bridge directory needed
- **Censorship resistance**: Bridges discovered through the Tox DHT network
- **Social trust model**: You control who can use your bridge
- **Easy sharing**: Share your bridge by adding friends on Tox

## Implementation Notes

- The provided `ToxClient` must already be started and bootstrapped to the Tox network
- toxpt does not manage the Tox client lifecycle - this is the responsibility of the embedding application
- The bridge does not close the Tox client on shutdown
- Friend list updates are reflected automatically when using dynamic mode
