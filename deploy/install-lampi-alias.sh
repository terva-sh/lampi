#!/bin/sh
# Optional installer for a `lampi` symlink to `terva-lampi`.
#
# The primary command stays terva-lampi. This script does not replace an
# existing lampi. A shell script, or a file that mentions neurobin, is
# called out as neurobin's LAMP installer and left in place.
# https://github.com/neurobin/lampi

set -eu

usage() {
	echo "usage: install-lampi-alias.sh [--bin PATH] [--dest PATH]" >&2
	echo "symlink terva-lampi as lampi. Default dest is ~/.local/bin/lampi." >&2
}

# looks_like_neurobin is true for a shell script or a file that names
# neurobin. The README treats either as a reason to leave `lampi` alone.
looks_like_neurobin() {
	path=$1
	if [ -L "$path" ]; then
		followed=$(readlink -f "$path" 2>/dev/null || true)
		if [ -n "$followed" ] && [ -f "$followed" ]; then
			path=$followed
		fi
	fi
	if [ ! -f "$path" ]; then
		return 1
	fi
	sig=$(head -c 2 "$path" 2>/dev/null || true)
	if [ "$sig" = "#!" ]; then
		return 0
	fi
	if grep -a -q -i neurobin "$path" 2>/dev/null; then
		return 0
	fi
	return 1
}

warn_existing() {
	path=$1
	if looks_like_neurobin "$path"; then
		echo "terva-lampi: $path looks like neurobin's LAMP installer (https://github.com/neurobin/lampi)." >&2
		echo "terva-lampi: left it in place. The primary command is terva-lampi." >&2
		return
	fi
	echo "terva-lampi: $path already exists and is not this program." >&2
	echo "terva-lampi: left it in place. The primary command is terva-lampi." >&2
}

bin=""
dest=""
while [ $# -gt 0 ]; do
	case "$1" in
	--bin)
		bin=${2:-}
		if [ -z "$bin" ]; then
			usage
			exit 2
		fi
		shift 2
		;;
	--dest)
		dest=${2:-}
		if [ -z "$dest" ]; then
			usage
			exit 2
		fi
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage
		exit 2
		;;
	esac
done

if [ -z "$bin" ]; then
	bin=$(command -v terva-lampi || true)
fi
if [ -z "$bin" ] || [ ! -f "$bin" ]; then
	echo "terva-lampi: terva-lampi was not found. Pass --bin." >&2
	exit 1
fi
if [ -d "$bin" ]; then
	echo "terva-lampi: --bin is a directory" >&2
	exit 1
fi

if [ -z "$dest" ]; then
	if [ -z "${HOME:-}" ]; then
		echo "terva-lampi: HOME is not set. Pass --dest." >&2
		exit 1
	fi
	dest="$HOME/.local/bin/lampi"
fi

if [ -L "$dest" ]; then
	target=$(readlink "$dest" || true)
	bin_real=$(readlink -f "$bin" 2>/dev/null || printf '%s' "$bin")
	dest_real=$(readlink -f "$dest" 2>/dev/null || printf '%s' "$target")
	if [ "$bin_real" = "$dest_real" ] || [ "$target" = "$bin" ]; then
		echo "terva-lampi: $dest already points at terva-lampi"
		exit 0
	fi
fi

if [ -e "$dest" ] || [ -L "$dest" ]; then
	warn_existing "$dest"
	exit 1
fi

mkdir -p "$(dirname "$dest")"
ln -s "$bin" "$dest"
echo "terva-lampi: linked $dest -> $bin"
echo "terva-lampi: the primary command is still terva-lampi"
