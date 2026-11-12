set working-directory := ".."
set default-list

# Capture a Session's prepared model request without calling a provider.
model-request *args:
    bash scripts/dump-model-request.sh {{ args }}

# Export the metadata database schema.
metadata-schema *args:
    bash scripts/dump-metadata-schema.sh {{ args }}
