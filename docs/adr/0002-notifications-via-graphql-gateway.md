# Notifications come from the Atlassian GraphQL gateway

Atlassian has no documented REST API for the notification feed (the bell panel). We use the `notifications` namespace of the Atlassian GraphQL gateway (`notificationFeed`, `markNotificationsByIdsAsRead/Unread`, `markNotificationsAsRead`), which is public but carries no stability guarantee. It is the same API Atlassian's own VS Code extension and Atlassify use.

## Consequences

- The client sits behind its own interface with queries pinned in `.graphql` files, so a schema change touches one place.
- An opt-in live test runs the real queries against a site.
- A schema break shows an error banner on the Notifications page; the rest of the app keeps working.
- Every feed query and mutation must pass `collabContextRoutingAri: "ari:cloud:platform::site/<cloudId>"`. Without it the feed silently returns empty and mutations fail with "Existing notification index was not found" (verified 2026-10-16).
- The legacy `notification-log` REST API was rejected: undocumented, session-cookie era.
