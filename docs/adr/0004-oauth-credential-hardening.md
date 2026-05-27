# OAuth credential hardening: no access token cache, byte zeroing, refresh rotation

Three hardening measures apply to all OAuth 2.0 credential handling:

1. **No in-memory access token cache.** Each sync operation loads the refresh token from the OS keyring, exchanges it for a fresh access token, uses it, then explicitly zeros the byte slice and discards it. Access tokens are never resident in memory between sync operations. Cost: one extra HTTPS round-trip per sync cycle (~200–500ms), which is negligible beside the network time of the sync itself.

2. **`[]byte` with explicit zeroing for all token values.** Go strings are immutable and the runtime may retain copies anywhere in the heap. All token values (access token, refresh token) are represented as `[]byte` and zeroed with `for i := range buf { buf[i] = 0 }` immediately after use. Token types do not implement `fmt.Stringer` to prevent accidental log leakage.

3. **Refresh token rotation.** Microsoft Graph (and other providers) issue a new refresh token on each use. The new token is written to the keyring and the old token is zeroed before the next operation proceeds. A compromised refresh token becomes invalid at the next legitimate sync.

The deciding constraint is that this app handles personal account data. A small, imperceptible speed cost is the correct tradeoff for eliminating credential residency windows.

## Considered Options

- **Cache access tokens for their full lifetime (~1 hour):** faster; access token in memory for up to an hour. Rejected: unnecessary residency window for personal account data.
- **No cache, no zeroing, no rotation:** simpler implementation. Rejected: leaves credentials in memory indefinitely and makes stolen refresh tokens permanently valid.
- **Chosen approach:** no cache + zeroing + rotation — imperceptible performance cost, minimal credential exposure window.
