set working-directory := ".."

cli *args:
    bash scripts/release-artifacts.sh build {{args}}

desktop *args:
    bash scripts/desktop-release.sh build --skip-install {{args}}

brew *args:
    bash scripts/update-brew-tap.sh {{args}}

[private]
_cli-build *args:
    bash scripts/release-artifacts.sh build {{args}}

[private]
_cli-verify-manifest *args:
    bash scripts/release-artifacts.sh verify-manifest {{args}}

[private]
_cli-verify-linux-static *args:
    bash scripts/release-artifacts.sh verify-linux-static {{args}}

[private]
_cli-smoke *args:
    bash scripts/release-artifacts.sh smoke-test {{args}}

[private]
_windows-installer-smoke *args:
    bash scripts/release-artifacts.sh smoke-windows-installer {{args}}

[private]
_installer-offer-smoke:
    bash scripts/release-artifacts.sh smoke-installer-offer

[private]
_windows-installer-offer-smoke:
    bash scripts/release-artifacts.sh smoke-windows-installer-offer

[private]
_windows-runtime-smoke:
    go test ./server/tools/shell -run '^TestWindowsChildExitStatus$'
    go test ./cli/kent -run '^(TestWindowsServiceRegistration|TestElevatedService|TestOrdinaryServiceInvocation)'

[private]
_desktop-build *args:
    bash scripts/desktop-release.sh build {{args}}

[private]
_desktop-assemble *args:
    bash scripts/desktop-release.sh assemble {{args}}
