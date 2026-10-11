set working-directory := ".."

deps *args:
    bash scripts/update-deps.sh {{ args }}
