## Connections and transcription

- The server must own speech capability, provider authentication, connection selection, transcription, input validation, and language handling.
- Speech capability must be built in and not user-overridable. Kent must support first-party OpenAI API, Codex subscription, and Grok routes with verified authenticated speech endpoints. Generic and OpenRouter speech access are outside this contract.
- Speech must use only capable members of the global default connection selection. Workspace, role, Agent, and Session selections must not affect speech.
- Each transcription invocation must use the server-wide connection-selection rules defined by [Provider Connections](provider-connections.md). Explicit Retry must count as a new invocation. Availability reads must not allocate a connection or probe credentials or providers.
- Kent must not automatically choose another connection after a provider or authentication failure. Missing credentials must not disable recording for a capable connection. Use must report its connection-specific error.
- Each connection must support an optional global configuration-file transcription-model setting. Built-in defaults must use the current best general multilingual model verified for the provider. Endpoints without a model choice must ignore the override. Speech must not add runtime model discovery, environment-variable overrides, or a model picker.
- Native Desktop must stream compressed audio to an opaque authenticated HTTP upload endpoint. Capability metadata and final results must use generated Protobuf contracts. The server must forward compressed audio to the provider while recording when the provider accepts streaming uploads. When a provider requires a completed file, the server may stage the received audio temporarily and upload it after recording stops.
- Speech must detect languages through the provider without forcing English.
- The client must use established browser or native recording APIs and compressed audio for saved recordings. Kent must not introduce WAV recording files or FFmpeg.
- Speech must be a narrow exception to client-independent server work. Client cancellation or loss of the speech connection must cancel its provider request and release server resources. This exception must not change other operations.

## Desktop fields and recording

- Every editable multiline Desktop field must offer speech except Workflow creation and Workflow editor fields. This includes Task create/edit descriptions, Task comments, manual Task-move instructions, chat prompts, chat composer, and question-answer/commentary fields.
- Read-only fields must not offer recording. When the global default selection has no speech-capable member, Desktop must hide microphone controls.
- In the chat composer, the microphone must appear between Agent Stop and Send. Other fields must place it at the bottom right, or to the left of an existing Save action. The field must reserve space so controls cannot cover editable text as the field grows.
- Desktop must support microphone capture on macOS, Windows, and Linux through platform microphone permission handling. Permission/setup must show a spinner. Missing system capture support, a missing microphone, or denied permission must show actionable Sonner feedback and return the control to its initial state.
- On Linux, Record must constitute consent for Kent's trusted microphone-only request without an additional permission prompt or permission store. The client must reject other origins and camera requests. System and device restrictions must apply.
- Preparation must be visible until capture is ready to accept speech. The recording must include audio from its first captured sample without trimming its prefix.
- Record and Stop must occupy the same position beside Save when present. Control changes must crossfade. The elapsed timer must slide in from the right and fade in. These transitions must respect reduced-motion settings.
- After capture begins, the microphone must become Stop and show elapsed time. Radial waveform bars must form a sound-reactive ring around Stop, rotated -90 degrees so the first low-frequency bar appears at the top. The noninteractive ring must render with a shadow as an overlay above nearby content, including adjacent controls, without reserving layout space. Adapting established component code is permitted. Kent must not implement its own FFT or use Lottie.
- Stop must end capture immediately. The control must then show a spinner until transcription finishes. Text input must accept edits throughout capture and transcription.
- Each navigation destination must own capture and request lifetime, independently of its widgets. Unmounting a field widget must not cancel the operation or invalidate a surviving logical draft. Closing the navigation destination must stop capture and cancel its speech request automatically.
- Each navigation destination must allow only one recording or transcription at a time. Only the recording field must show Stop. Other microphone controls must be disabled with a reason until the operation finishes, including transcription. Separate navigation destinations must not coordinate microphone access.
- If the server connection drops while recording, the client must continue local capture. It must not automatically replay provider work while recording. After Stop, it must attempt transcription from the saved recording and present recovery only if that attempt fails.
- Kent's recording upper bound must be ten minutes. A verified smaller provider limit must reduce it. Kent must use the strictest applicable duration and size bounds among eligible default connections.
- Reaching either bound must stop capture as if the operator pressed Stop. Desktop must include the complete final encoded chunk before deciding whether the recording fits the upload limit. When the finalized recording fits, Desktop must attempt transcription. When it exceeds the upload limit, Desktop must keep the complete original and show the existing recording recovery dialog with a size-limit error. Kent must not truncate, split, or re-encode the recording to fit. Desktop must show a visible notification.
- At an automatic limit stop, Desktop must use native system sound where supported. When native sound is unavailable or fails, Desktop must attempt the approved embedded alert sound. If neither can play, Desktop must show Sonner feedback with diagnostics. Sound failure must not block capture stopping or transcription.
- This contract must not display provisional transcription. Live text preview belongs to a separate feature.

## Final text and no-ops

- The server must treat transcripts without any Unicode letter or digit as successful no-ops. This includes empty, whitespace-only, punctuation-only, symbol-only, and emoji-only results.
- Empty recordings and recordings shorter than one second must be discarded as successful no-ops. Silence may be detected through simple existing microphone-level readings without an additional analysis model or complicated algorithm. Immediate streaming need not guarantee zero provider charges for discarded recordings.
- A usable transcript must be appended exactly to the field's current text, including edits made during recording. For a nonempty field without a trailing blank line, Desktop must add a blank-line separator.
- Insertion must not move the active caret or change the selection. Speech must not automatically send, save, or submit.
- Chat Send may accept submissions during speech. If sending clears the same composer, final speech must append to its current cleared or edited text as the next message. When this behavior is unavailable, Chat Send must be disabled with an explanatory tooltip during speech.
- Other fields must disable their Save/Submit actions with an explanatory tooltip while their speech action is active.

## Recording preservation and recovery

- Desktop must keep one compressed original recording in an OS-cleaned client temporary directory until successful insertion, successful no-op, or explicit Discard. A server staging file required for upload must be removed after use and must not become another recovery recording.
- Transcription failure, destination closure, and interrupted transport must not delete usable audio.
- A transcription failure must open a persistent, blocking recovery dialog above app content. It must show the finalized recording path and offer Retry, Show in folder, Save a copy, and Discard. Only one recovery dialog may be visible.
- The recovery dialog must be a narrow exception to ordinary Sonner-only operational errors and destination presentation. It must not be clipped by the originating sidebar or page.
- Retry must use the same finalized recording without asking the operator to record again.
- If the originating field exists, Retry success must append there. If the field is absent, Desktop must copy the transcript to the clipboard and show a banner.
- Clipboard success must not delete the recording before explicit Discard. If clipboard copy fails, debug builds must panic. Production must keep the dialog open and show the transcript in a scrollable field with an actionable explanation.
- Kent must not restore recovery after app restart. Closing the app while recovery is open may lose its recovery state. The dialog must show the path. Kent need not guarantee file availability after OS cleanup. The operator can use Save a copy for permanent storage.
