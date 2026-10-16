# latui

A terminal app for day-to-day Atlassian Cloud work: reading notifications and acting on Jira issues without the web UI.

## Language

### Sign-in

**Site**:
One Atlassian Cloud instance, identified by its `*.atlassian.net` host.
_Avoid_: Instance, tenant, server

**Me**:
The signed-in Atlassian user whose notifications and issues are shown.
_Avoid_: Current user, self

### Notifications

**Notification**:
One entry in Me's Atlassian notification feed, describing something an actor did to a Jira issue or Confluence page.
_Avoid_: Alert, event, activity

**Direct**:
The notification category for things aimed at Me (mentions, assignments, replies to Me).
_Avoid_: Personal, mine

**Watching**:
The notification category for activity on issues or pages Me watches but was not directly addressed in.
_Avoid_: Following, subscribed

**Unread**:
A notification Me has not marked read; shown with a dot.
_Avoid_: New, unseen

**Unread filter**:
The toggle that hides notifications that are already read.
_Avoid_: Only show unread

**Notification group**:
Notifications about the same target shown as one row, headed by the newest; acting on the group acts on every notification in it.
_Avoid_: Thread, bundle

**Target**:
The Jira issue or Confluence page a notification is about.
_Avoid_: Entity, object, link

### Jira

**Issue**:
A Jira work unit identified by a key such as `DEMO-42`.
_Avoid_: Ticket, work item, task (except as an issue type)

**Issue detail**:
The screen showing one issue's description, fields, comments and history.
_Avoid_: Issue view, ticket page

**Summary**:
An issue's one-line title.
_Avoid_: Title, name, subject

**Description**:
An issue's long rich-text body.
_Avoid_: Body, details

**Comment**:
A rich-text message on an issue by one author.
_Avoid_: Reply, note

**Transition**:
A move of an issue from one status to another, as allowed by its workflow.
_Avoid_: Status change, move

**History**:
The record of field changes made to an issue, who made them and when.
_Avoid_: Changelog, activity, audit log
