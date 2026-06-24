# Registering an Azure app for Outlook calendar sync

To connect an Outlook (Microsoft Graph) calendar, PyCalendar needs an
**Application (client) ID** from a free Azure app registration. This is a
one-time, **free** setup — **no paid Azure subscription is required**. The app
registration costs nothing; you are only registering a public client so the
Microsoft identity platform will issue tokens for your personal account.

You then paste that client ID into `config.toml`. No client secret is involved —
PyCalendar is a public client and uses PKCE (see ADR-0008).

## Portal steps

1. Go to the [Azure portal](https://portal.azure.com) and open
   **Microsoft Entra ID** (formerly Azure Active Directory).
2. Select **App registrations** → **New registration**.
3. **Name**: anything you like, e.g. `PyCalendar`.
4. **Supported account types**: choose
   **Personal Microsoft accounts only** (consumers).
   (PyCalendar uses the `/consumers` authority.)
5. Leave **Redirect URI** empty here for now and click **Register**.
6. On the new app's overview page, copy the **Application (client) ID** — this is
   the value you put in `config.toml`.

### Authentication (redirect URI)

7. Open **Authentication** → **Add a platform** →
   **Mobile and desktop applications**.
8. Add the redirect URI `http://localhost`.
   The Microsoft identity platform ignores the port for loopback redirects, so
   this one entry covers the ephemeral port PyCalendar listens on (ADR-0008).
   PyCalendar actually redirects to `http://127.0.0.1:<port>` (the IP literal —
   RFC 8252 §8.3 prefers it over the name `localhost` to avoid IPv4/IPv6
   resolution ambiguity); the identity platform treats `127.0.0.1` and
   `localhost` as the same loopback address, so the `http://localhost` entry
   matches. If a sign-in ever fails with `redirect_uri` mismatch, add a second
   redirect URI `http://127.0.0.1` as a belt-and-suspenders entry.
9. Confirm the app is a **public client** — do **not** create a client secret.
   (If an "Allow public client flows" toggle is present, it does not need to be
   enabled for the PKCE authorization-code flow, but leaving the platform as
   *Mobile and desktop applications* is what matters.)

### API permissions

10. Open **API permissions** → **Add a permission** → **Microsoft Graph** →
    **Delegated permissions**.
11. Add:
    - `Calendars.ReadWrite`
    - `offline_access`
12. These are normal-consent permissions for personal accounts, so no admin
    consent is required — you'll consent in the browser during the first
    "Connect Microsoft account" sign-in.

## Configure PyCalendar

Put the copied client ID into your `config.toml` (see the data/config locations
in `CLAUDE.md`):

```toml
[oauth]
microsoft_client_id = "00000000-0000-0000-0000-000000000000"
```

No built-in client ID ships with PyCalendar, so this value must be set before
"Connect Microsoft account" will work. If it is missing, the Add Calendar dialog
shows: *"Set microsoft_client_id in config.toml — see
docs/azure-app-registration.md"*.

## Connecting

In the app: **Settings → Calendars → Add Calendar**, set **Type** to **Outlook**,
click **Connect Microsoft account**, sign in and consent in the browser that
opens, then pick which Outlook calendar to sync and click **Save**.
