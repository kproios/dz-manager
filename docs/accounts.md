# Accounts, sign-in and roles

DZ Manager keeps its own accounts. Nobody needs an identity provider: people sign in with a
password or their Steam account, and every sign-in also asks for a code from an authenticator app
(2FA). Roles and invites live in DZ Manager, under **Users**.

## First owner

On a new install there are no accounts. On the server, run (same environment as the service):

```bash
dzm admin invite-owner                    # Docker: docker exec dzm dzm admin invite-owner
```

It prints a one-time link, valid for a day. Open it, pick your name and a password (or Steam), then
set up 2FA. Run the command again whenever every owner has lost their phone *and* their recovery
codes: it makes a new owner account, and the old one can then be reset under Users.

## Inviting people

**Users → Invite someone**: pick the role, an optional note, and how long the link stays valid
(1, 2 or 7 days). The link works once and is shown only then. The person picks their name and how
they sign in, then sets up 2FA. Open invites can be cancelled.

## Sign-in methods

| Method | Notes |
|---|---|
| Password | At least 12 characters. The sign-in name is the display name in lower case. |
| Steam | On by default; `DZM_STEAM_LOGIN=0` turns it off. Only Steam accounts linked to a DZ Manager account get in. |
| Single sign-on (OpenID Connect) | Optional: set `DZM_OIDC_ISSUER`, `DZM_OIDC_CLIENT_ID`, `DZM_OIDC_CLIENT_SECRET` (redirect URI `<DZM_PUBLIC_URL>/auth/callback`) and `DZM_OIDC_LABEL` for the button. The provider only proves who someone is; their role comes from DZ Manager, and the account must be invited or linked first. |

Every method is followed by the authenticator code (or a one-time recovery code). Each account can
link several methods under **Account**, as long as one is left.

## Roles

| Permission | Owner | Admin | Moderator | Viewer |
|---|---|---|---|---|
| Live view (players, map, chat, events, performance) | ✓ | ✓ | ✓ | ✓ |
| Private player data (IP addresses, Steam checks) | ✓ | ✓ | ✓ | – |
| Message and kick players | ✓ | ✓ | ✓ | – |
| Bans, review queue, priority queue | ✓ | ✓ | ✓ | – |
| Safe actions (teleport, heal, vehicle repair/refuel/flip/stop) | ✓ | ✓ | ✓ | – |
| Strong actions (kill, strip, give items, delete vehicle) | ✓ | ✓ | – | – |
| Lock, kick everyone, restarts | ✓ | ✓ | – | – |
| Settings (messages, Discord, map, intel, retention) | ✓ | ✓ | – | – |
| Server profiles, RCON, ingest tokens, API keys | ✓ | – | – | – |
| Users, roles, the full audit log | ✓ | – | – | – |

Everyone sees their own entries in the audit log; owners see all of it. There is always at least
one owner, and nobody can change, disable or remove their own account from the Users page.

Every panel API route has a permission in `internal/perm`. A route without one is refused for
everyone, and a test fails the build when a route is added without a rule.

## Extra protection

- **Fresh 2FA for sensitive actions.** Users, server profiles, API keys, strong game actions, bans,
  kick-all and "restart now" need a 2FA code from the last 10 minutes; the panel asks for it.
- **Throttling.** Five failed sign-ins lock the account for 30 seconds, doubling up to an hour;
  20 failures from one address in 15 minutes block that address. A code can be used only once.
- **Sessions** last 12 hours (2 hours idle). **Account** lists them and can sign out the others.
  Changing someone's role, disabling them or resetting their 2FA ends their sessions at once.
- **API keys** belong to the user who made them and never do more than that user's role allows,
  at every request. A disabled user's keys stop working.
- Passwords are stored as argon2id hashes, 2FA secrets encrypted with `DZM_SECRET_KEY`, recovery
  codes and session tokens as keyed hashes.
