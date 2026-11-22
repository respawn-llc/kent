## File schema compatibility

- Kent must support migrations from older supported Workflow file schema versions to the latest schema version.
- Each new migration must receive explicit product-owner approval.
- Supported schema versions have no scheduled removal date. Kent may remove support only in a major release and with an explicit decision to stop supporting those versions.
- Opening a Workflow file and saving it must write the latest schema version.
- Kent must not support downgrading Workflow files.
- An older Kent must refuse to open a Workflow file whose schema version is newer than it supports.

## Discovery and Project links

- The global Workflow Library must list global Workflow files and a Recent files list.
- Global Workflow discovery must use the active Kent persistence root's `workflows` directory.
- Project Workflow discovery must browse one explicitly selected attached workspace at a time.
- Workspace discovery must read that workspace's `.kent/workflows` directory without recursively scanning repositories or managed Worktrees.
- Discovering a Workflow must not automatically link it to a Project.
- Project Workflow Links must be explicit.
- Projects must link Workflows by file location.
- Each Project Workflow Link must record its selected file location.
- Task operations must use the applied Workflow definition.
- Opening another file must not change existing Project Workflow Links.
- If a linked file is missing, Kent must offer locating the file without deleting Tasks.
- The Project attachment picker must suggest global Workflow files, Workflow files in the Project's default workspace, and Recent files.
- The attachment picker must read only the default workspace automatically.
- Browsing another workspace must read only the selected workspace.
- Choosing a file outside those suggestions must be supported.
- Selecting a suggestion must attach its file location without copying its definition.
- The first attachment of a Workflow must apply its initial definition.
- Attaching an already-applied Workflow to another Project must use its applied definition and show the ordinary notice for any source update.

## Linked locations

- A Workflow file inside an attached workspace must be linked by that workspace and its relative file path.
- A Workflow file outside attached workspaces must be linked by its absolute server-side path.
- Changing a Project's default workspace must not retarget existing links.
- If an attached workspace's root changes, its relative Workflow links must follow that workspace.

## File content

- YAML must be the single native Workflow file format.
- Native Workflow files must use the `.kwf` extension.
- A Workflow file must contain its schema version, definition revision, and Workflow identity.
- The Workflow file must own its name, description, Execution Target Policy, and applicable custom Git ref.
- The Workflow file must own its complete authored Nodes, Node Groups, Transitions, Transition Branches, prompts, Parameters, Assignee names, thinking-selection rules, Approval settings, continuation settings, Join settings, and Script paths.
- Project links and defaults, remembered file locations, Recent files, Tasks, Sessions, pending answers and Approvals, worktree locations, and local activity dates must belong to local Kent state.
- Role definitions, role system prompts, Provider Connections, credentials, and executable scripts must be external to the Workflow file.
- Kent must accept unknown extra fields, ignore them during execution, and keep them on Save without warnings.
- Known values and supported behavior types must undergo ordinary validation.
- Save must omit fields whose values equal their schema defaults.
- The default values of `assignee_selection` and `thinking_selection` must be named `default`.

## Transition prompts

- Schema version 1 must represent an inline Transition Prompt with a `text` key containing its text content, without a separate type discriminator.
- A prompt must contain exactly one supported variant key.
- Schema version 1 must support only the `text` variant.
- A prompt must not contain both `text` and `path`.
- The `.NodeId` prompt variable must be a permanent alias of `.NodeKey` and return the readable Node key.

## Editing and external changes

- Save must atomically replace the selected Workflow file.
- The editor's Save and Apply action must write the file first and then apply that file.
- If the file saves but Apply fails, Kent must report that the file was saved but not applied.
- A saved file whose Apply fails must use the ordinary external-update notice and Apply action. Kent must not roll back the file or introduce a separate reconciliation flow.
- Kent must keep YAML comments when saving. Formatting normalization is allowed.
- When a file changes while the editor has local changes, Kent must offer a warning dialog with Cancel, Save copy, Overwrite, and Drop local changes.
- The user must be able to force-overwrite the file from that dialog.
- Users must be able to edit Workflows while Tasks run.
- File edits must not change the applied Workflow definition without explicit user approval.
- External edits must not proactively stop running agents.
- If changed Transitions invalidate an agent's completion, Kent must report the error to the agent.
- That error must tell the agent to inspect the applied Transitions through UUID-based CLI inspection when its system message differs from the applied definition.
- Workflow changes must not leave Tasks permanently unrepairable. Users must be able to recover through Resume or Manual Move.
- The editor must check the file when opening, regaining focus, explicitly reloading, and saving. It must not require a filesystem watcher.
- The selected Workflow board must also check the source file when opening, regaining focus, and explicitly refreshing.
- The board and editor must show a persistent Apply notice when a source update is available.
- Task transitions must not check the source file for updates.

## Applying definitions

- Kent must keep one applied Workflow definition per Workflow UUID, shared by Projects using that Workflow.
- Each Workflow must have only one applied revision. Kent must replace obsolete applied revisions without keeping snapshot history.
- Kent must keep the current applied definition when no Project is linked to it.
- Apply must replace the complete applied definition atomically. It must not apply selected parts of a candidate.
- Apply must read and validate the current file when invoked, rather than a candidate frozen when the update notice appeared.
- Compatible changes may apply while Tasks run.
- Apply must block changes that invalidate existing Task references.
- Apply must use the same Workflow validation and Task-impact rules for editor saves and external file updates.
- An unchanged definition revision must not count as an update, even when the file contents differ.
- Applying an update must require a higher definition revision than the applied revision.
- Kent must keep the applied file's content hash and definition revision.
- If the source file is missing, unreadable, malformed, or uses an unsupported schema, Kent must show a persistent source error and use the last successfully applied definition for ordinary execution and new Task starts.
- Apply must be unavailable until the source is repaired or relocated.

## Creation and copying

- New Workflow creation must offer a file location chooser.
- Project-originated creation must default to the default workspace's `.kent/workflows` directory.
- Global Library creation must default to the active Kent persistence root's `workflows` directory.
- Users must be able to choose another location.
- Project-originated creation must save and attach the file. It must make the Workflow the Project default only when that Project has no default.
- Save copy must create a new Workflow UUID at definition revision 1 and leave existing links unchanged.
- Save copy must open the system location picker with a prefilled location and filename.
- The proposed copy filename must be `<original>-N.kwf`, where `N` is the copy number.

## Deletion

- Workflow deletion confirmation must offer Cancel, Delete tasks only, and Delete tasks and the file.
- A missing Workflow file must not delete its Tasks.
- Deletion from a Project board must delete only that Project's Tasks for the selected attachment.
- Delete tasks only must leave the file and Project link available.
- Delete tasks and the file must warn about other Projects attached to that file and leave their Tasks untouched.
- Delete tasks and the file must unlink the Workflow from the current Project.
- Other linked Projects must keep their applied execution and use the ordinary missing-source warning.
- Workflow deletion must be available only in Project context. The global Library must offer Remove from Recent, not Workflow deletion.
- If Task deletion succeeds but file deletion fails, Kent must report the partial completion through an error Toast without rolling back Task deletion.

## Listing and opening

- Recent files queries must use remembered metadata without accessing the files.
- Project attachment lists must use remembered metadata without accessing Workflow files.
- Global and workspace discovery must enumerate only the selected Workflow directory and show filenames and paths without parsing each file.
- Discovery may use a remembered display name when available.
- File opening, path-based inspection, and file validation must read the selected Workflow file.
- Kent must report missing-file, unreadable-file, invalid-file, and unsupported-schema errors when the user tries to open the path.
- Valid Workflow files with execution errors must be openable for repair.
- Opening an invalid file must not rewrite it.

## Agent authoring

- The CLI must not expose Workflow definition editing commands.
- Agents must author Workflow definitions through direct file edits.
- Read APIs for validation and inspection must be available.
- Workflow inspection must follow the [CLI inspection contract](cli-commands.md#workflow-selection-and-discovery).
- CLI Workflow selectors must use file paths for linking, unlinking, default selection, Task creation, and Workflow-filtered Task lists. Inspection must support the separate path and UUID meanings defined in the [CLI inspection contract](cli-commands.md#workflow-selection-and-discovery).
- Operations that require a Project attachment must validate that attachment for the selected path.
- The CLI must offer Apply for a selected Workflow file.
- Invoking CLI Apply must count as approval and execute immediately, including when an agent invokes it.

## Dependency validation

- Kent must reuse existing configuration, Workflow, and Session launch validation for external dependencies.
- File and editor Draft validation must evaluate the candidate definition. Validation for Task execution must evaluate the applied definition.
- The editor must show applicable existing validation in its selected workspace context.
- Missing Assignees must be visible in their existing picker and replaceable with configured roles.

## Desktop file opening

- Desktop must register as a `.kwf` file handler on every supported Desktop platform.
- Opening a Workflow file must not attach it to a Project or start work.
- With a local server, Desktop must support native file opening and Save location pickers.
- With a remote server, opening and attaching Workflows must use server-side paths.
- If a desktop-local file is opened while connected remotely, Kent must launch and show an editor placeholder explaining that the file must be placed on the server before it can be opened.
- Opening a desktop-local file must not automatically upload or copy it to a remote server.
- OS file-open events must reuse the existing application window and editor.
- Opening the same path must focus its existing Draft.
- Before switching away from an unsaved Draft, Kent must offer Save, Discard, and Cancel.

## Relocating linked files

- Locate file must require the same Workflow UUID as the applied definition.
- Locate file must update the shared source location for all linked Projects.
- Locating a source with a newer revision must show the ordinary Apply notice without applying it automatically.
- Switching to a different Workflow must use ordinary attachment and Task moves.

## Pending Approvals

- A Task awaiting Transition Approval must keep that pending Approval until the user approves it or manually moves the Task.
- Approve must resolve the Transition against the current applied Workflow definition without a frozen definition or Approval-specific Workflow revision.
- If the current Transition requires no Approval, the existing Approve action must allow that ordinary Transition to execute.
- Approve authorizes the Task to resume through the originally selected Transition at its source Node. Its current branches, targets and instructions apply, including branches added or replaced while the Task was paused.
- If the submitted Parameters do not match the current Transition, Approve must leave the Task awaiting Approval and show an error Toast directing the user to manually move the Task.
- If the selected Transition is absent from the source Node or has no branches, Kent must keep the Approval and its submitted inputs and show the choice as unavailable.
- Approving an unavailable choice must leave the Approval pending and direct the user to manually move the Task.
- Kent must reject new pending Approvals whose selected Transition or branches are missing.
- A pending Approval must protect only its source Session from explicit deletion.
- If current Approval context is unavailable, Approve must leave the Approval pending and direct the user to manually move the Task.

## Configuration context

- Workflow execution must use the Task's workspace configuration.
- An editor opened from a Project must validate against the selected Project workspace.
- An isolated editor must use the known workspace containing the Workflow file, or global settings when no containing workspace is known.
- The editor must identify its validation context.
- Moving a Workflow file must not change the role configuration used by existing linked Tasks.

## Initial migration

- Kent must prevent use until the one-time migration from database-owned Workflow definitions finishes.
- The initial migration must write Workflow files into the active Kent persistence root's `workflows` folder.
- The initial migration must derive readable filenames from normalized Workflow names, such as `main-software-engineering.kwf`.
- Migrated filenames must avoid reserved operating-system filenames.
- When Workflow names produce the same normalized filename, the migration must use numeric suffixes such as `main-software-engineering-2.kwf` and `main-software-engineering-3.kwf`.
- The migration must use the Workflow UUID as the filename only when the name is missing or cannot produce a usable filename.
- If a previous attempt wrote the expected file, the migration must reuse that identical file.
- If a destination file has different content, the migration must stop without overwriting it and identify the path.
- Resolving that collision must require moving the conflicting file aside before retrying.

## File identity

- Assigning the same UUID to different Workflows is a user error.
- Only the Workflow ID must use a UUID in the Workflow file. Graph references must use readable keys.
- Renaming a Node key through the GUI must update affected Task references to the new key.
- Renaming a Node key through direct file edits must undergo ordinary Apply validation.
- GUI key renames must update affected saved references for every graph entity, including Transitions and Transition Branches.
- The immediate GUI Save and Apply must use its explicit key-rename information to update those references.
- If a later Apply lacks that rename information, it must use ordinary external-file validation and may block on references to old keys.
- Kent must not persist rename information for retry or reconcile lost rename information.
- GUI key renames must not rewrite sent Session conversation content.
- Kent must not infer renames from direct file edits.
- Opening a Workflow file must open the selected path.
- Attach and Apply must check the selected file's UUID and location against already-known Workflows without scanning for other files.
- Two Projects using the same source location must share its Workflow identity.
- If another known file claims the same Workflow UUID, Attach and Apply must report a validation error asking the user to fix the UUID.
- If a linked source file changes its Workflow UUID, Apply must report a source-identity error without changing the existing applied Workflow. Repair must restore or locate that Workflow's source.
- Users must be able to open conflicting files for repair.
- The editor must remember the path of the file being edited.
- Recent files entries must identify files by path.
- Opening a Recent files entry must open its remembered path.
- If that file is missing, Kent must show a dialog that offers to remove the Recent files entry.

## Definition revisions

- A Workflow file must contain a definition revision separate from its schema version.
- For a changed Save, Kent must set the definition revision to one greater than the higher of the file revision and the applied revision.
- A no-op Save must leave the definition revision unchanged.
- Direct file editors must increment the definition revision once per logical edit. Failing to increment it is a user error.
- Restoring older definition content must use a new higher revision.
