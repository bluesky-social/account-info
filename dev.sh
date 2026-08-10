#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec nix --extra-experimental-features 'nix-command flakes' develop "path:${root}" "$@"
