#!/bin/sh
# install.sh installs a terva-lampi release on Linux or macOS
# (TKT-01M3HS0FX3). It follows git-ticket's installer: the source is the
# GitHub releases of the public mirror, the archive is checked against
# checksums.txt before anything is unpacked, and the script never sudos.
#
#   curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | sh -s -- --register
#
# --register runs `terva-lampi register --install-service` once the binary
# is in place. Under `curl | sh` the script itself is stdin, and register
# reads its code from stdin when stdin is not a terminal, so register is
# given the terminal instead. With no terminal, --register is refused
# before anything is downloaded.

set -u

REPO="terva-sh/lampi"
API="${TERVA_LAMPI_INSTALL_API:-https://api.github.com}"
DOWNLOAD="${TERVA_LAMPI_INSTALL_DOWNLOAD:-https://github.com/$REPO/releases/download}"

fail() {
    echo "install.sh: $*" >&2
    exit 1
}

# A function rather than `sed "$0"`, because under `curl | sh` $0 is the
# shell, not this file.
usage() {
    cat <<'EOF'
usage: install.sh [--prefix DIR] [--version TAG] [--register [--lake NAME]]

Installs terva-lampi from the latest GitHub release, or from TAG, after
checking its sha256 against the release's checksums.txt.

  --prefix DIR   where the binary goes. Default: the first of ~/.local/bin
                 and ~/bin that exists or can be created. Never sudo.
  --version TAG  a release tag such as v0.1.0, to match a lake's version.
  --register     then run `terva-lampi register --install-service`, which
                 asks for the registration code on the terminal.
  --lake NAME    passed to register, for a machine joining a second lake.

From a pipe:  curl -fsSL .../install.sh | sh -s -- --register
EOF
}

# --- arguments ---------------------------------------------------------
PREFIX=""
TAG=""
REGISTER=false
LAKE=""
while [ $# -gt 0 ]; do
    case "$1" in
    --prefix)
        [ $# -ge 2 ] || fail "--prefix needs a directory"
        PREFIX="$2"
        shift 2
        ;;
    --version)
        [ $# -ge 2 ] || fail "--version needs a tag"
        TAG="$2"
        shift 2
        ;;
    --register)
        REGISTER=true
        shift
        ;;
    --lake)
        [ $# -ge 2 ] || fail "--lake needs a name"
        LAKE="$2"
        shift 2
        ;;
    -h | --help)
        usage
        exit 0
        ;;
    *)
        usage >&2
        fail "unknown argument $1"
        ;;
    esac
done
[ -z "$LAKE" ] || [ "$REGISTER" = true ] || fail "--lake only applies with --register"

# Checked first, so a machine without a terminal learns before anything
# is downloaded or replaced. Opening /dev/tty is the test: the node can
# exist in a session that has no controlling terminal.
if [ "$REGISTER" = true ]; then
    (: </dev/tty) 2>/dev/null ||
        fail "--register needs a terminal for the registration code; run 'terva-lampi register' on one after installing"
fi

# --- platform ----------------------------------------------------------
case "$(uname -s)" in
Linux) OS="linux" ;;
Darwin) OS="darwin" ;;
*)
    fail "unsupported platform $(uname -s); Windows takes the zip from https://github.com/$REPO/releases"
    ;;
esac

case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*)
    fail "no release is built for $(uname -m)"
    ;;
esac

command -v curl >/dev/null || fail "curl is required"
command -v tar >/dev/null || fail "tar is required"

# sha256sum on Linux, shasum on macOS. Installing unverified is not
# offered: a pipe from a forge is exactly what a checksum is for.
if command -v sha256sum >/dev/null; then
    SHA="sha256sum"
elif command -v shasum >/dev/null; then
    SHA="shasum -a 256"
else
    fail "neither sha256sum nor shasum is available, and unverified installs are not offered"
fi

# --- release -----------------------------------------------------------
if [ -z "$TAG" ]; then
    TAG=$(curl -fsSL "$API/repos/$REPO/releases/latest" |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
    [ -n "$TAG" ] || fail "could not read the latest release tag from $API"
fi
case "$TAG" in
v*) ;;
*) TAG="v$TAG" ;;
esac

VER="${TAG#v}"
ASSET="terva-lampi_${VER}_${OS}_${ARCH}.tar.gz"
BASE="$DOWNLOAD/$TAG"

# --- download and verify -----------------------------------------------
TMP=$(mktemp -d) || fail "mktemp failed"
trap 'rm -rf "$TMP"' EXIT

echo "downloading $ASSET ($TAG)"
curl -fsSL -o "$TMP/$ASSET" "$BASE/$ASSET" || fail "downloading $ASSET failed"
curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "downloading checksums.txt failed"

# The grep narrows checksums.txt to this asset, and an asset missing
# from it fails the same way a wrong sum does.
(
    cd "$TMP" &&
        grep " $ASSET\$" checksums.txt | $SHA -c - >/dev/null 2>&1
) || fail "sha256 verification failed for $ASSET; refusing to install it"
echo "sha256 verified against checksums.txt"

mkdir "$TMP/unpack" || fail "mktemp failed"
tar -xzf "$TMP/$ASSET" -C "$TMP/unpack" || fail "unpacking $ASSET failed"
[ -f "$TMP/unpack/terva-lampi" ] || fail "the archive did not contain a terva-lampi binary"

# --- destination -------------------------------------------------------
if [ -n "$PREFIX" ]; then
    DEST="$PREFIX"
    mkdir -p "$DEST" 2>/dev/null || fail "cannot create $DEST"
    [ -w "$DEST" ] || fail "$DEST is not writable; pick another --prefix"
else
    DEST=""
    for d in "$HOME/.local/bin" "$HOME/bin"; do
        if mkdir -p "$d" 2>/dev/null && [ -w "$d" ]; then
            DEST="$d"
            break
        fi
    done
    [ -n "$DEST" ] || fail "neither ~/.local/bin nor ~/bin is writable; pass --prefix DIR"
fi

# Staged beside the target and renamed over it, so a running agent keeps
# its open binary and a failed copy never leaves half a file on PATH.
# The staged copy has to run before it replaces anything, so a binary
# this machine cannot execute leaves the previous install in place.
STAGE="$DEST/.terva-lampi.install.$$"
cp "$TMP/unpack/terva-lampi" "$STAGE" || { rm -f "$STAGE"; fail "copying into $DEST failed"; }
chmod 0755 "$STAGE" || { rm -f "$STAGE"; fail "chmod failed"; }
GOT=$("$STAGE" --version 2>/dev/null) || { rm -f "$STAGE"; fail "the downloaded binary does not run here; nothing was replaced"; }
mv -f "$STAGE" "$DEST/terva-lampi" || { rm -f "$STAGE"; fail "installing into $DEST failed"; }

# --- report ------------------------------------------------------------
echo "installed $DEST/terva-lampi"
echo "  $GOT"

case ":$PATH:" in
*":$DEST:"*) ;;
*)
    echo ""
    echo "NOTE: $DEST is not on your PATH. Add it, for example:"
    echo "  export PATH=\"$DEST:\$PATH\""
    ;;
esac

# An agent that was already running keeps the old binary until it
# restarts. Say how, rather than restart a service behind the caller.
if [ "$REGISTER" = false ]; then
    if [ -f "$HOME/.config/systemd/user/terva-lampi-agent.service" ]; then
        echo ""
        echo "restart a running agent onto this release:"
        echo "  systemctl --user restart terva-lampi-agent.service"
    elif [ -f "$HOME/Library/LaunchAgents/sh.terva.lampi.agent.plist" ]; then
        echo ""
        echo "restart a running agent onto this release:"
        echo "  launchctl kickstart -k gui/$(id -u)/sh.terva.lampi.agent"
    else
        echo ""
        echo "next, join a lake with a code from 'terva-lampi serve register' on the lake host:"
        echo "  $DEST/terva-lampi register --install-service"
    fi
    exit 0
fi

# --- register ----------------------------------------------------------
echo ""
echo "registering: paste the code from 'terva-lampi serve register' when asked"
if [ -n "$LAKE" ]; then
    "$DEST/terva-lampi" register --install-service --lake "$LAKE" </dev/tty
else
    "$DEST/terva-lampi" register --install-service </dev/tty
fi
