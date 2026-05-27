# Microsoft Graph API for Outlook integration

Outlook Calendar sync uses the Microsoft Graph API, not Exchange CalDAV.

Exchange CalDAV works with app-passwords and would reuse the CalDAV adapter, but Microsoft is actively deprecating it for consumer accounts and it is unreliable for Microsoft 365 work accounts. Microsoft Graph handles both personal Outlook.com and work Microsoft 365 accounts uniformly under a single OAuth 2.0 flow, and exposes richer event data (Teams join links, meeting metadata) that Exchange CalDAV does not surface.

The Graph adapter is a distinct protocol adapter behind the sync engine's adapter seam — callers see `FetchChanges`, `PushChange`, `DeleteRemote` regardless of protocol.

## Considered Options

- **Exchange CalDAV** — reuses the CalDAV adapter; app-password auth; no Azure app registration needed. Rejected: deprecated for consumer accounts, unreliable for M365, misses rich event metadata.
- **Microsoft Graph API (chosen)** — OAuth 2.0 public client (PKCE); Azure app registration required; covers both Outlook.com and M365 uniformly.
