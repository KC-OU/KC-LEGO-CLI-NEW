# shellcheck shell=bash
# Sourced by promote-dev.sh and promote-dashboard.sh: the PIN that lets `-y` skip the typed CONFIRM, like sudo.
# The PIN is stored as a salted hash (mode 600) in $PIN_FILE; set it once with: bash scripts/promote-dev.sh --set-pin
# A PIN never replaces the preflight or the tests: -y only answers the CONFIRM prompts.
PIN_FILE="${WMS_PROMOTE_PIN_FILE:-/root/.config/wms-go/promote.pin}"

pin_hash() { printf '%s' "$1$2" | sha256sum | cut -d' ' -f1; } # salt, pin

pin_ok() { # $1 = the PIN to test
	local salt hash
	[ -f "$PIN_FILE" ] || return 1
	read -r salt hash <"$PIN_FILE" || return 1
	[ "$(pin_hash "$salt" "$1")" = "$hash" ]
}

pin_set() {
	local a b salt
	read -r -s -p "New promote PIN (at least 6 characters): " a
	echo
	[ "${#a}" -ge 6 ] || { echo "too short" >&2; return 1; }
	read -r -s -p "Again: " b
	echo
	[ "$a" = "$b" ] || { echo "they differ" >&2; return 1; }
	mkdir -p "$(dirname "$PIN_FILE")"
	salt="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
	(umask 077 && printf '%s %s\n' "$salt" "$(pin_hash "$salt" "$a")" >"$PIN_FILE")
	echo "PIN saved to $PIN_FILE"
}

# pin_ask: three tries; on success PROMOTE_PIN holds the PIN for the child script (never printed).
pin_ask() {
	local try p
	[ -f "$PIN_FILE" ] || { echo "no PIN set yet: run  bash scripts/promote-dev.sh --set-pin  first." >&2; return 1; }
	for try in 1 2 3; do
		read -r -s -p "Promote PIN: " p || return 1
		echo
		if pin_ok "$p"; then
			PROMOTE_PIN="$p"
			export PROMOTE_PIN
			return 0
		fi
		echo "wrong PIN ($try/3)" >&2
		sleep 2
	done
	return 1
}
