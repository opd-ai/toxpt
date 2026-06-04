# toxpt

`github.com/opd-ai/toxpt` is a pure-Go Tor pluggable transport skeleton that wraps Tor cell payloads in framed Tox messages and enforces friend-only bridge access.

## Security model

- Friend ACL checks happen before inbound cell payloads are accepted.
- Public key ACL comparisons use constant-time comparison (`crypto/subtle.ConstantTimeCompare`).
- Tox is initialized in secure mode with TCP-only transport (`UDPEnabled=false`) to avoid UDP bypass/leak behavior documented by `toxcore`.
- Framing uses a fixed 4-byte big-endian length prefix with payload limits.

## Usage

```go
cfg := toxpt.DefaultConfig()
cfg.ToxSecretKey = mySecret
cfg.AllowedFriends = [][32]byte{friendPublicKey}

bridge, err := toxpt.NewEmbeddableBridge(cfg)
if err != nil {
    panic(err)
}

ctx := context.Background()
if err := bridge.Start(ctx); err != nil {
    panic(err)
}
defer bridge.Stop()
```
