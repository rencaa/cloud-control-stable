#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  echo "Usage: $0 VERSION CHANNEL DOWNLOAD_URL SHA256 [ROLLOUT_PERCENT] [NOTES]" >&2
  exit 2
fi

VERSION="$1"
CHANNEL="$2"
DOWNLOAD_URL="$3"
SHA256_VALUE="${4,,}"
ROLLOUT_PERCENT="${5:-10}"
NOTES="${6:-}"
KEY_DIR="${CLOUD_RELEASE_KEY_DIR:-./release/keys}"
PRIVATE_KEY="$KEY_DIR/client-update-ed25519-private.pem"
PUBLIC_KEY="$KEY_DIR/client-update-ed25519-public.pem"

mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"
if [[ ! -f "$PRIVATE_KEY" ]]; then
  openssl genpkey -algorithm Ed25519 -out "$PRIVATE_KEY"
  openssl pkey -in "$PRIVATE_KEY" -pubout -out "$PUBLIC_KEY"
  chmod 600 "$PRIVATE_KEY"
fi

MESSAGE_FILE="$(mktemp)"
SIGNATURE_FILE="$(mktemp)"
trap 'rm -f "$MESSAGE_FILE" "$SIGNATURE_FILE"' EXIT
printf '%s\n%s\n%s\n%s\n' "$VERSION" "$CHANNEL" "$DOWNLOAD_URL" "$SHA256_VALUE" > "$MESSAGE_FILE"
openssl pkeyutl -sign -inkey "$PRIVATE_KEY" -rawin -in "$MESSAGE_FILE" -out "$SIGNATURE_FILE"

SIGNATURE="$(base64 -w0 < "$SIGNATURE_FILE")"
PUBLIC_KEY_BASE64="$(openssl pkey -pubin -in "$PUBLIC_KEY" -outform DER | tail -c 32 | base64 -w0)"
OUTPUT_DIR="${CLOUD_RELEASE_MANIFEST_DIR:-./release/manifests}"
OUTPUT_FILE="$OUTPUT_DIR/client-release-$VERSION-$CHANNEL.json"
mkdir -p "$OUTPUT_DIR"

python3 - "$VERSION" "$CHANNEL" "$DOWNLOAD_URL" "$SHA256_VALUE" "$SIGNATURE" "$ROLLOUT_PERCENT" "$NOTES" "$OUTPUT_FILE" <<'PY'
import json, sys
version, channel, url, digest, signature, rollout, notes, output = sys.argv[1:]
with open(output, "w", encoding="utf-8") as f:
    json.dump({
        "version": version,
        "channel": channel,
        "download_url": url,
        "sha256": digest,
        "signature": signature,
        "rollout_percent": int(rollout),
        "notes": notes,
    }, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
echo "Manifest: $OUTPUT_FILE"
echo "CLOUD_UPDATE_PUBLIC_KEY=$PUBLIC_KEY_BASE64"
