---
status: proposed
---

# v1 signs in by reusing acli's OAuth token

v1 has no sign-in flow of its own. It reads the OAuth token that Atlassian's official CLI (`acli jira auth login --web`) stores in the OS secure store (Windows Credential Manager target `acli:jira:<cloudId>:<accountId>`, gzip JSON) and calls Jira REST and the notifications GraphQL gateway with it as a Bearer token. Verified 2026-10-16: the token's Jira scopes (`read:jira-work`, `write:jira-work`, …) are enough for both, including mark-as-read.

latui cannot refresh the token itself (refresh needs acli's client secret), so it re-reads the store before each call and, on expiry, runs an acli command to make acli refresh it.

## Considered Options

- API token (email + token, Basic auth) stored in the OS keychain with an in-app sign-in form: deferred to v1.1. `LATUI_EMAIL` / `LATUI_API_TOKEN` env vars are accepted in v1 as an escape hatch.
- Own OAuth 3LO app: rejected — needs app registration and a hosted secret for a personal tool.

## Consequences

- acli's storage format is not a public interface; an acli update can break sign-in. The credential source sits behind one interface so the API-token path can replace it.
- macOS Keychain and Linux Secret Service reads go through `go-keyring` and are untested.
