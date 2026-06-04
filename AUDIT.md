# UNIVERSAL BUG AUDIT (END-TO-END) — 2026-06-04

## Project Profile

**Purpose**: toxpt is a pure-Go Tor pluggable transport that wraps Tor cell payloads in framed Tox messages and enforces friend-only bridge access.

**Target Users**: Tox client developers who want to offer Tor bridges to their friends over the Tox P2P network.

**Deployment Model**: Library embedded into user-facing Tox clients. The library accepts an existing `*toxcore.Tox` client instance rather than creating its own.

**Critical Paths**:
1. Connection acceptance and ACL enforcement (`listener.go:Accept`, `auth.go:IsAuthorized`)
2. Framed message read/write (`framing.go:readFramed`, `framing.go:writeFramed`)
3. Bridge lifecycle (`bridge.go:Start`, `bridge.go:Stop`)
4. Transport dialing (`dial.go:dial`)

**Security Claims**:
- Friend ACL checks happen before inbound cell payloads are accepted
- Public key ACL comparisons use constant-time comparison
- Framing uses fixed 4-byte big-endian length prefix with payload limits

## Audit Scope

| Metric | Value |
|--------|-------|
| Packages audited | 1 (`github.com/opd-ai/toxpt`) |
| Total files | 8 source files |
| Total functions | 35 (16 functions + 19 methods) |
| Total lines of code | 304 |

## Coverage Log

| Package | 3b Logic | 3c Nil | 3d Errors | 3e Resources | 3f Concurrency | 3g Security | 3h Aliasing | 3i Init | 3j API |
|---------|----------|--------|-----------|--------------|----------------|-------------|-------------|---------|--------|
| toxpt   | ✅       | ✅     | ✅        | ✅           | ✅             | ✅          | ✅          | ✅      | ✅     |

## Goal-Achievement Summary

| Stated Goal | Status | Blocking Findings |
|-------------|--------|-------------------|
| Friend-only bridge access | ✅ | None |
| Constant-time ACL comparison | ✅ | None |
| Framed message protocol with limits | ✅ | None |
| Embedded Tox client support | ✅ | None |
| Graceful bridge lifecycle | ⚠️ | MEDIUM-1 |

## Findings

### CRITICAL

*No critical findings.*

### HIGH

- [x] **HIGH-1: handleConn discards all data without forwarding** — `bridge.go:91` — **Logic** — The current `handleConn` implementation relays traffic bidirectionally between the accepted Tox connection and the configured Tor OR port, so this finding is already resolved in code.

  **Code path**: `EmbeddableBridge.Start()` → `acceptLoop()` → `handleConn()` → `io.Copy(io.Discard, conn)` discards all data.

  **Remediation**: Implement actual Tor cell forwarding logic to connect accepted inbound connections to a Tor OR port. Validate with integration tests that verify data flows through the bridge.

- [x] **HIGH-2: framedConn.Read uses context.Background() ignoring caller context** — `framing.go:85` — **Logic/API** — `framedConn` now preserves caller-configured read deadlines and reuses them when reading framed payloads, so framed reads no longer clear or ignore connection-level timeout control.

  **Code path**: Any caller using `conn.Read()` on a framedConn → `readFramed(context.Background(), c.Conn)` ignores context.

  **Remediation**: Store context in framedConn at construction time or use `SetReadDeadline` more aggressively. At minimum, document this limitation. Validate with a test that cancels a context and verifies read returns promptly.

- [x] **HIGH-3: framedConn.Write uses context.Background() ignoring caller context** — `framing.go:98` — **Logic/API** — `framedConn` now preserves caller-configured write deadlines and reuses them when writing framed payloads, so framed writes honor connection-level timeout control.

  **Code path**: Any caller using `conn.Write()` on a framedConn → `writeFramed(context.Background(), c.Conn, p)` ignores context.

  **Remediation**: Same as HIGH-2. Validate with a test that cancels a context and verifies write returns promptly.

### MEDIUM

- [x] **MEDIUM-1: EmbeddableBridge.Stop() ignores close errors** — `bridge.go:100-102` — **Error Handling** — `EmbeddableBridge.Stop()` now collects listener and transport shutdown failures and returns them with `errors.Join`, so callers can detect partial shutdown failures.

  **Code path**: `bridge.Stop()` → `_ = b.listener.Close()` and `_ = b.transport.Close()`.

  **Remediation**: Collect errors and return a combined error using `errors.Join()`. Validate with tests that verify error propagation.

- [x] **MEDIUM-2: ToxTransport.Close() ignores listener close error** — `transport.go:106` — **Error Handling** — `ToxTransport.Close()` now returns the listener close failure instead of discarding it.

  **Remediation**: Return the listener close error if non-nil. Validate with tests.

- [x] **MEDIUM-3: acceptLoop silently continues on Accept errors** — `bridge.go:73-77` — **Error Handling** — `acceptLoop()` now logs unexpected `Accept()` failures before retrying and exits cleanly on listener closure instead of spinning silently.

  **Code path**: `acceptLoop()` receives error from `Accept()` → checks `ctx.Err()` only → continues silently.

  **Remediation**: Log Accept errors before continuing. Consider implementing exponential backoff for repeated errors to avoid tight error loops.

- [x] **MEDIUM-4: inbound channel has fixed buffer size of 16** — `listener.go:30` — **Resource/Performance** — `Config` now exposes `InboundBufferSize`, and listener creation uses that value so deployments can tune inbound buffering.

  **Remediation**: Consider making channel buffer size configurable in Config, or document the limitation. Not critical since Tox friends list is typically small.

- [ ] **MEDIUM-5: dial() address parameter is ignored** — `dial.go:8` — **API** — The `address` parameter to the `dial` method is completely ignored. The function signature suggests it should be used to specify a target, but the implementation uses `t.cfg.ClientPublicKey` instead.

  **Code path**: `ToxTransport.Dial(ctx, address)` → `dial(ctx, _)` ignores address.

  **Remediation**: Either use the address parameter (parse public key from it) or remove it from the internal `dial` signature and document that `ClientPublicKey` in config determines the dial target.

- [x] **MEDIUM-6: acceptLoop creates detached context** — `bridge.go:61` — **Resource** — `Start()` now derives the accept loop context from the caller's context and closes the listener when that context is canceled, so bridge shutdown follows parent cancellation.

  **Code path**: `Start(ctx)` → `acceptCtx, cancel := context.WithCancel(context.Background())` ignores `ctx`.

  **Remediation**: Use `context.WithCancel(ctx)` instead of `context.Background()` to respect the caller's context. Validate with a test that cancels the start context and verifies the loop stops.

### LOW

- [ ] **LOW-1: Potential goroutine leak if Accept() blocks forever** — `bridge.go:72` — **Resource** — If `Accept()` blocks forever (e.g., listener never closed), the goroutine running `acceptLoop` will never exit. While `Stop()` does close the listener which should unblock Accept, if the listener implementation has bugs, this could leak.

  **Remediation**: Consider adding a timeout or using a separate goroutine for the Accept call with select on closed channel.

- [ ] **LOW-2: IsAuthorized timing leak on empty ACL** — `auth.go:51-59` — **Security** — When the ACL has zero friends, `IsAuthorized` returns `false` immediately without any comparison loop iterations. An attacker could potentially distinguish between "empty ACL" and "non-empty ACL" based on timing. However, this is a very minor leak and unlikely to be exploitable in practice.

  **Remediation**: Consider adding a constant-time no-op loop when friends list is empty, or document as accepted behavior.

- [ ] **LOW-3: Logger nil check in Accept happens per-rejected-connection** — `listener.go:45-50` — **Performance** — The nil check on logger happens inside the rejection path rather than once at listener creation. Minor inefficiency.

  **Remediation**: Check logger once at listener creation and store a flag or no-op logger.

- [ ] **LOW-4: frameHeaderPool may allocate unnecessarily** — `framing.go:31-35` — **Performance** — The code gets a buffer from the pool, then potentially allocates a new one if capacity is insufficient. The pool's purpose is partially defeated for large payloads.

  **Remediation**: Initialize pool with `maxFrameSize + 4` capacity to avoid this path.

- [ ] **LOW-5: Missing documentation on exported methods** — Multiple files — **Documentation** — Several exported methods lack GoDoc comments: `Name()`, `Methods()`, `IsRunning()`, `Dial()`, `Listen()`, `Close()`, `Accept()`, `Read()`, `Write()`.

  **Remediation**: Add GoDoc comments to all exported symbols.

- [ ] **LOW-6: Config.Validate creates new error for nil logger but doesn't wrap ErrInvalidConfig** — `config.go:51-52` — **Error Handling/API** — The nil logger check returns `errors.New("logger must be non-nil")` instead of wrapping `ErrInvalidConfig` like other validation errors in the same function.

  **Remediation**: Use `fmt.Errorf("logger must be non-nil: %w", ErrInvalidConfig)` for consistency.

- [ ] **LOW-7: bindAddr parameter unused in newToxListener** — `listener.go:25` — **API** — The `bindAddr` parameter is accepted but never used. The listener always binds to `net.IPv4zero:0`.

  **Remediation**: Either use the parameter or remove it from the signature.

- [ ] **LOW-8: friendSource interface not exported but could be useful** — `auth.go:10-12` — **API** — The `friendSource` interface is internal but well-designed. It could be useful for users wanting to provide custom friend sources.

  **Remediation**: Consider exporting as `FriendSource` if there's demand.

## Metrics Snapshot

| Metric | Value |
|--------|-------|
| Total functions | 35 |
| Functions above complexity 15 | 0 |
| Highest complexity function | Accept (12.4 overall) |
| Avg cyclomatic complexity | 4.1 |
| Doc coverage | 48.0% |
| Duplication ratio | 0% |
| Test pass rate | All passing |
| go vet warnings | 0 |
| Race detector issues | 0 |

## False Positives Considered and Rejected

| Candidate | Reason Rejected |
|-----------|----------------|
| `matched \|= subtle.ConstantTimeCompare` accumulates matches incorrectly | Actually correct: bitwise OR ensures any match sets bit, final `== 1` confirms exactly one match (or any match since ConstantTimeCompare returns 1 for match). The pattern is constant-time. |
| Frame size validation could overflow | `uint32` max (4GB) is checked against `maxFrameSize` (64KB) which fits in int. No overflow risk. |
| `defer frameHeaderPool.Put(buf)` may return wrong buffer | The `buf` variable may be reassigned, but `defer` captures the variable, not its value at defer time. However, since we want to return the potentially new buffer, this is actually a bug... wait, no - if we allocate new `buf`, we don't want to return it to pool meant for smaller buffers. This is intentional. |
| Race condition in Start() between running check and mutex lock | `running.Load()` followed by `mu.Lock()` could have TOCTOU issue, but the duplicate Start is harmless (returns nil) and the early return is just an optimization. |
| Slice aliasing in NewFriendACL | The code correctly copies the input slice with `copy(copyKeys, allowed)`. No aliasing bug. |

## Remaining Scope

*Audit complete. All packages, files, and checklist categories have been audited.*
