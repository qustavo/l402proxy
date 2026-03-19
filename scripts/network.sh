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
	nigiri start --ln
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

	echo -n "✓ Waiting for CLN to be ready "
	until [[ $(nigiri cln getinfo | jq -r .blockheight) != 0 ]]; do
		sleep 1
		echo -n "."
	done
	echo

	echo -n "✓ Funding onchain wallets"
	nigiri faucet lnd 1
	nigiri faucet cln 1

	echo "Opening channel LND -> CLN"
	CLN_PUBKEY=$(nigiri cln getinfo | jq -r .id)
	nigiri lnd connect $CLN_PUBKEY@cln:9935
	nigiri lnd openchannel --node_key $CLN_PUBKEY --local_amt 10000000
	nigiri rpc -generate 6 # mature the channel

	echo -n "Waiting for channel to be active "
	until [[ $(nigiri lnd listchannels | jq '.channels[0].active') == true ]]; do
		sleep 1
		echo -n "."
	done
	echo

	echo "Sending 10k sats from LND to CLN"
	INVOICE=$(nigiri cln invoice 10000000 $RANDOM test | jq -r .bolt11)
	nigiri lnd payinvoice $INVOICE -f

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
