set working-directory := ".."
set default-list

# Update dependencies; pass --help for ecosystem and version options.
deps *args:
    bash scripts/update-deps.sh {{ args }}
