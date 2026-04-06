#!/bin/bash
set -eu

# network.sh — start/stop Docker network for integration tests using nigiri

usage() {
	cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  start    Start bitcoind + two LND nodes (alice, bob)
  stop     Stop and remove all containers and the network

Examples:
  $(basename "$0") start
  $(basename "$0") stop
EOF
}

cmd_start() {
	echo "Starting l402 integration network..."
	local nigiri_flags="--ln"
	if [[ -n "${CI:-}" ]]; then
		nigiri_flags="$nigiri_flags --ci"
	fi

	nigiri start $nigiri_flags
	echo

	echo -n "✓ Waiting for LND to become active "
	until [[ $(nigiri lnd state | jq -r .state) == 'SERVER_ACTIVE' ]]; do
		sleep 1
		echo -n "."
	done
	echo

	echo -n "✓ Waiting for LND to sync with the chain "
	until [[ $(nigiri lnd getinfo | jq -r .synced_to_chain) == 'true' ]]; do
		sleep 1
		echo -n "."
	done
	echo

	# Wait for chopsticks (faucet service) to be ready before trying to fund wallets.
	# Chopsticks may not respond to requests immediately after container starts.
	echo -n "✓ Waiting for faucet service "
	until curl -s http://localhost:3000 > /dev/null 2>&1; do
		sleep 1
		echo -n "."
	done
	echo

	echo -n "✓ Funding onchain wallets"
	nigiri faucet lnd 1 btc
	nigiri faucet cln 1 btc
	nigiri rpc -generate 6

	echo -n "✓ Waiting for CLN to be ready "
	until [[ $(nigiri cln getinfo | jq -r .warning_bitcoind_sync) == 'null' ]]; do
		sleep 1
		echo -n "."
	done
	until [[ $(nigiri cln getinfo | jq -r .warning_lightningd_sync) == 'null' ]]; do
		sleep 1
		echo -n "."
	done
	echo

	# Wait for LND certificates and macaroons to be written to volumes.
	# Containers may report "ready" before files are actually written to the host filesystem,
	# causing tests to fail when trying to read credentials. This wait ensures files exist.
	echo -n "✓ Waiting for LND credentials to be written "
	until [[ -f ~/.nigiri/volumes/lnd/tls.cert && -f ~/.nigiri/volumes/lnd/data/chain/bitcoin/regtest/admin.macaroon ]]; do
		sleep 1
		echo -n "."
	done
	echo

	# Generate and save CLN rune for authentication in tests
	echo -n "✓ Generating CLN rune "
	nigiri cln createrune | jq -r .rune > ~/.nigiri/volumes/lightningd/rune
	echo
	echo "✓ Rune saved to ~/.nigiri/volumes/lightningd/rune"

	echo "Opening channels LND <-> CLN"
	CLN_PUBKEY=$(nigiri cln getinfo | jq -r .id)
	nigiri lnd connect $CLN_PUBKEY@cln:9935
	nigiri lnd openchannel --node_key $CLN_PUBKEY --local_amt 10000000
	nigiri rpc -generate 6 # mature the channel
	sleep 1

	nigiri cln fundchannel `nigiri lnd getinfo | jq -r .identity_pubkey` 10000000
	nigiri rpc -generate 6 # mature the channel

	echo -n "Waiting for channel to be active "
	until [[ $(nigiri lnd listchannels | jq '.channels[0].active') == true ]]; do
		sleep 1
		echo -n "."
	done
	echo

	echo "✓ Network ready to test"
}

cmd_stop() {
	echo "Stopping l402 integration network..."
	nigiri stop --delete
	echo "✓ Network stopped"
}

main() {
	if [[ $# -eq 0 ]]; then
		usage
		exit 1
	fi

	case "$1" in
		start)
			cmd_start
			;;
		stop)
			cmd_stop
			;;
		restart)
			cmd_stop
			cmd_start
			;;
		-h|--help|help)
			usage
			exit 0
			;;
		*)
			echo "Error: unknown command '$1'"
			usage
			exit 1
			;;
	esac
}

main "$@"
