# Microsoft Graph OAuth via loopback redirect + PKCE (S256)

PyCalendar obtains its first Microsoft Graph refresh token using the RFC 8252
**loopback redirect** + **PKCE (S256)** authorization-code flow. On "Connect
Microsoft account", the app:

1. generates a PKCE verifier/challenge and a `crypto/rand` `state`;
2. starts an `http.Server` bound to `127.0.0.1:0` (ephemeral port); the chosen
   port determines `redirect_uri = http://127.0.0.1:<port>/callback`;
3. opens the system browser at the `/authorize` endpoint with `response_type=code`,
   `code_challenge`, `code_challenge_method=S256`, `state`, and
   `prompt=select_account`;
4. receives the redirect on the loopback listener, verifies `state`, reads the
   `code`;
5. POSTs the `authorization_code` grant (with the `code_verifier`) to the token
   endpoint and stores the resulting refresh token in the CredentialStore;
6. shuts the listener down.

This **supersedes the OAuth-callback mechanism described in ADR-0005** (custom
`pycalendar://` URI scheme activated by Windows, single-instance named-mutex,
named-pipe IPC to forward the redirect to the running instance).

## Why ADR-0005's rejection of loopback was wrong

ADR-0005 rejected the localhost loopback redirect for two reasons; both are
incorrect:

- **"Firewall noise."** Binding to `127.0.0.1` (not `0.0.0.0`) does **not**
  trigger the Windows Defender Firewall prompt — that prompt only appears for
  listeners reachable off-machine. A loopback-only listener is invisible to the
  firewall.
- **"Dynamic port requires Azure to whitelist a pattern."** The Microsoft
  identity platform explicitly supports loopback redirect URIs and **ignores the
  port** for `http://localhost` / `http://127.0.0.1` redirects, matching only on
  host and path. A single registered `http://localhost` redirect covers every
  ephemeral port, so no per-port registration is needed.

## Why loopback + PKCE wins

- **Less code.** No URI-scheme registration in the installer, no single-instance
  mutex, no named-pipe IPC, no second-process activation path.
- **Works in dev.** `go run .` can complete a real login; the named-pipe path
  required an installed, scheme-registered binary.
- **Cross-platform.** The same loopback listener works on Linux/macOS, where
  `pycalendar://` registration would need separate per-OS plumbing.
- **Public client, no secret.** PKCE removes the need for a client secret, so the
  app ships as a public client and stores no confidential material.

## Scope boundary

The single-instance mutex / named-pipe IPC and the `pycalendar://` (and `.ics`)
**file-association** launch path remain a **separate, later slice** — they are
still wanted for double-click `.ics` open, just no longer on the OAuth critical
path. This ADR only changes how the OAuth redirect is captured.

## Testability

Every external endpoint (authorize URL, token URL) and the browser-opener are
injectable on `graphauth.Config`, so the entire flow is verified against an
`httptest` mock identity provider with no live Azure app: the test drives the
`/authorize` → loopback `/callback` → `/token` round-trip and asserts the
token endpoint received a `code_verifier` whose S256 hash equals the earlier
`code_challenge`.

## Considered Options

- **Custom URI scheme + single-instance + named pipe (ADR-0005, chosen then):**
  rejected now — far more code, no dev-build support, Windows-only, and its two
  stated advantages over loopback do not hold once the loopback rejection is
  corrected.
- **Loopback redirect + PKCE (chosen):** minimal code, no firewall prompt, works
  in `go run`, cross-platform, public client with no secret, fully testable.

## Related

- ADR-0004 — OAuth credential hardening (no access-token cache, `[]byte` zeroing,
  refresh rotation). The tokens returned by `graphauth.Login` are `[]byte` and
  zeroed by the UI after the refresh token is stored.
- ADR-0005 — superseded for the OAuth-callback mechanism; its single-instance /
  file-association concerns survive as a future slice.
