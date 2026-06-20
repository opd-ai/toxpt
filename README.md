# toxpt

`github.com/opd-ai/toxpt` is a pure-Go Tor pluggable transport that wraps Tor cell payloads in framed Tox messages and enforces friend-only bridge access.

## Design Philosophy

toxpt is designed to be embedded in user-facing Tox clients, allowing Tox users to offer Tor bridges to their friends over the Tox P2P network. Instead of creating its own Tox instance, toxpt accepts an existing `*toxcore.Tox` client, enabling seamless integration with Tox applications.

## Security model

- Friend ACL checks happen before inbound cell payloads are accepted.
- Public key ACL comparisons use constant-time comparison (`crypto/subtle.ConstantTimeCompare`).
- The bridge uses the existing Tox client's security configuration with the following security features enabled by default:
  - **Noise-IK encryption**: Authenticated key agreement protocol providing mutual authentication
  - **Forward secrecy (double-ratchet)**: Enabled automatically through async pre-key exchanges with friends
  - **Async messaging with pre-key exchange**: Provides forward-secure communication using Ed25519 signing keys
  - **TCP transport**: Required for reliable async messaging and forward secrecy
- Framing uses a fixed 4-byte big-endian length prefix with payload limits.
- If no explicit friend list is provided, the bridge dynamically allows all friends from the Tox client.
- Legacy encryption fallback is disabled for maximum security - all connections use Noise-IK or better.

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
// TCP is automatically enabled (StartPort and EndPort default to 33445-33545)
toxClient, err := toxcore.New(opts)
if err != nil {
    panic(err)
}
defer toxClient.Kill()

// Verify security posture
posture := toxClient.GetSecurityPosture()
// Should show: NoiseIKEnabled=true, EffectiveSecurityLevel="noise-ik"
// ForwardSecureEnabled will become true after pre-key exchanges with friends

// Bootstrap to the Tox network
if err := toxClient.BootstrapDefaults(); err != nil {
    panic(err)
}

// Optional: Set up security callbacks for friend key changes
// This allows you to handle forward-secure key rotations
toxClient.OnFriendKeyChange(func(friendPK, oldKey, newKey [32]byte) {
    // A friend's signing key has changed - verify via out-of-band mechanism
    // then call toxClient.MarkFriendSignKeyVerified(friendPK, newKey)
})

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
// while also serving as a Tor bridge to your friends with secure encryption
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
- **End-to-end encrypted**: All connections use Noise-IK authenticated encryption

## Security Considerations

### Transport Security
- **Noise-IK Protocol**: All Tox connections use Noise-IK authenticated key agreement by default
- **Forward Secrecy**: Enabled through async pre-key exchanges with friends using Ed25519 signing keys
- **TCP Recommended**: Use TCP (enabled by default) rather than UDP for better reliability with forward-secure messaging
- **UDP Disabled Recommended**: For Tor bridge use, disable UDP to avoid traffic analysis vulnerabilities

### Key Rotation
- When using forward-secure messaging, friends' signing keys are automatically rotated through pre-key exchanges
- If a friend's signing key changes unexpectedly, the `OnFriendKeyChange` callback will fire
- Verify key changes through out-of-band mechanisms (e.g., comparing safety numbers) before accepting new keys

### Trust Model
- The bridge enforces Friend-only ACLs - only Tox friends can use the bridge
- Friend public keys are verified through Tox's Curve25519 key agreement
- Additional key verification through the safety-number mechanism provides defense against active attacks

## Implementation Notes

- The provided `ToxClient` must already be started and bootstrapped to the Tox network
- toxpt does not manage the Tox client lifecycle - this is the responsibility of the embedding application
- The bridge does not close the Tox client on shutdown
- Friend list updates are reflected automatically when using dynamic mode
- The Tox client's security configuration is automatically inherited:
  - Noise-IK encryption is used by default
  - Forward secrecy is enabled through async pre-key exchanges when friends connect
  - To verify security posture, call `ToxClient.GetSecurityPosture()` after creating the instance
- TCP transport should be enabled (default) for reliable async messaging and forward secrecy
- For Tor bridge use, UDP should be disabled (`options.UDPEnabled = false`) to avoid traffic analysis

Donate Monero(The only good cryptocurrency) to support development
==================================================================

 - `monero:43H3Uqnc9rfEsJjUXZYmam45MbtWmREFSANAWY5hijY4aht8cqYaT2BCNhfBhua5XwNdx9Tb6BEdt4tjUHJDwNW5H7mTiwe`

