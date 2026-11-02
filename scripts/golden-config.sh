#!/bin/sh
# golden-config.sh — write the minimal config skeleton the golden-set runner
# needs (models list is filled by the A1_MODEL / A1_API_KEY / A1_BASE_URL
# env overrides the config loader applies). CI calls this with HOME pointed
# at a scratch dir; local users with a real ~/.a1/config.yaml never need it.
set -eu

A1_DIR="${A1_DIR:-${HOME}/.a1}"
mkdir -p "$A1_DIR"

if [ -f "$A1_DIR/config.yaml" ]; then
	exit 0
fi

cat > "$A1_DIR/config.yaml" <<'EOF'
models:
  - name: golden-model
    api_key: placeholder-overridden-by-env
    context_window: 128000
    default: true
EOF

echo "golden: wrote $A1_DIR/config.yaml skeleton (env overrides supply the model)"
