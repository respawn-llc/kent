## Mobile stack

| Area                     | Choice                                                                                                       |
| ------------------------ | ------------------------------------------------------------------------------------------------------------ |
| Platforms                | Kotlin Multiplatform for Android (minSdk 29) and iOS (deployment target 26.0)                                |
| UI and screen state      | Compose Multiplatform with Material 3. FlowMVI containers own screen state independently of Compose.         |
| Dependency injection     | Metro                                                                                                        |
| Error handling           | ApiResult                                                                                                    |
| Form validation          | KMP Utils `inputforms` for fast checks of user-entered values.                                               |
| API                      | Square Wire generates Kotlin DTOs from Kent’s Protobuf schemas into `commonMain`.                            |
| Networking               | Ktor Client with OkHttp on Android and Darwin on iOS. Use Kent’s binary WebSocket protocol for API messages. |
| Navigation               | Decompose                                                                                                    |
| Local storage            | SQLDelight with SQLite for features that need relational storage. Draft storage is a separate decision.      |
| Background notifications | Firebase Cloud Messaging via a privately operated, closed-source push relay.                                 |
| Crash reporting          | Firebase Crashlytics, enabled by default with an opt-out setting.                                            |
| Unit tests and lint      | Kotest without mocks, Detekt for Kotlin, and Android Lint.                                                   |

## Build and module boundaries

Kent mobile uses one Kotlin Multiplatform build under `apps/mobile`, with shared Gradle conventions, thin Android and iOS entry points, and a Metro composition root. Create a module when it has production code to own. Keep shared mobile components in that build.

- Domain modules own client contracts and models. Server business rules belong on the Kent server.
- Data modules implement domain contracts and coordinate data sources.
- Infrastructure modules own platform APIs, network transport, and storage. The composition root binds implementations to contracts.
- Feature modules own FlowMVI containers and their Compose Multiplatform screens. Keep shared logic and containers independent of Compose so a future Swift UI can use them. Use Material 3 for the shared UI.

## API and storage

Read operation metadata from the generated Protobuf descriptor set. Follow the [server API contract](specs/server-api-contract.md) when decoding responses and events. For fast feedback on user input, use KMP Utils and small Kotlin validators for checks such as UUID or URL syntax.

Use ApiResult for structured recoverable errors. Use SQLDelight over SQLite when a feature requires relational storage.

## Contributor tools

Use KSRC for Kotlin dependency source inspection. Write unit tests with Kotest without mocks.

## Gradle configuration

Use `sh.kent.mobile` as the official Android application ID and iOS bundle identifier. Supply app identifiers, Firebase project settings, and the push relay endpoint through build or deployment configuration. Supply privileged push credentials only to the relay for that deployment. From a clean checkout, a contributor must be able to build and run the debug app against their own server without organization access, Firebase configuration, or release signing credentials. Configure Firebase to test push delivery or crash uploads.

Pin Gradle with the wrapper in `apps/mobile`. Pin Kotlin and Android Gradle Plugin versions in `apps/mobile/gradle/libs.versions.toml`. Put shared build settings in `apps/mobile/gradle.properties`.

## Android CI

- On a pull request that changes the mobile build or its inputs, run Android Lint, Detekt, and Kotest unit tests on the host JVM. Assemble the Android debug app.
- When those changes reach `main`, assemble the Android release app with R8 enabled. This job builds the artifact without publishing it.
- Generate the startup Baseline Profile in a periodic job. After regeneration, assemble the R8-optimized release and check the artifact for the compiled profile.
