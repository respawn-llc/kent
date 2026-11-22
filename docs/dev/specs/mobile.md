## Platform support

- The Android app must support Android 10 (API level 29) and later.
- The iOS app must support iOS 26.0 and later.

## Connection

For build and module decisions, see [Mobile architecture](../mobile-architecture.md).

- The mobile app must connect to one already configured and operational Kent server.
- Users must be able to enter the server address manually and change it in Settings.
- If the server is unreachable or its protocol version differs from the app's, the connection screen must explain the failure, let the user edit the address, and offer Retry.
- After the first successful connection, the app must open Inbox.
- Changing the configured server must restart the mobile app against the new address.
- Users must provide network reachability and protection for their connection.
- Mobile must follow the [server API contract](server-api-contract.md).
- Mobile must require a live connection for server operations.
- Mobile must keep unsent drafts across app restarts.

## Layout

- Android and iOS must use one adaptive interface for phones, tablets, and foldables.
- When space permits two panes, users must be able to open any page in the secondary pane with its own navigation stack.
- When a vertical hinge separates the two panes, the app must place one pane on each side.
- When the layout narrows to one pane with a secondary destination open, the app must show that destination full screen. Back must close it and reveal the destination that was on the primary pane.

## Inbox

- Inbox must list Task-scoped attention. Selecting an item must open Task Detail to view the Task, answer a Question, or respond to an Approval.
- Standalone Session prompts belong in Session Chat.
- The app must keep an unfinished answer until the user explicitly clears it or the Question is resolved. After leaving Task Detail or restarting the app, the app must restore the draft only when the user reopens the same pending Question on the same server.

## Notifications and diagnostics

- Mobile must support background attention notifications on Android and iOS.
- Mobile must enable each push category only when its tap destination is available.
- Notification content and tap destinations must match the [Desktop attention contract](desktop-gui.md#inbox-questions-approvals-and-notifications).
- Crash reporting must default to enabled and offer an opt-out setting.
