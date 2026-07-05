#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/catfish-dero"

mkdir -p "$DIST/bin"
cd "$ROOT"

GOOS=windows GOARCH=amd64 go build -o "$DIST/bin/derod.exe" ./cmd/derod
GOOS=windows GOARCH=amd64 go build -o "$DIST/bin/dero-miner.exe" ./cmd/dero-miner
GOOS=windows GOARCH=amd64 go build -o "$DIST/bin/dero-wallet-cli.exe" ./cmd/dero-wallet-cli
GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o "$DIST/CatfishDero.exe" ./cmd/catfish-desktop

cat > "$DIST/README.txt" <<'EOF'
Catfish DERO Desktop

Double-click CatfishDero.exe to start the local DERO test chain client.
The app starts the node and wallet automatically. CPU mining starts only after clicking the mining button.

Data directory:
  %LOCALAPPDATA%\CatfishDeroPublic

This package is for testing and evaluation.
DERO upstream source uses a RESEARCH license; confirm licensing before commercial distribution.
EOF

echo "Built: $DIST/CatfishDero.exe"
