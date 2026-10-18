#!/usr/bin/env bash
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
source ./public-access.sh

ip() {
  case "$*" in
    '-4 route get 1.1.1.1') printf '%s\n' "${TEST_ROUTE:-}";;
    '-4 -o address show scope global') printf '%s\n' "${TEST_ADDRESSES:-}";;
    *) return 1;;
  esac
}
expect() { local got; got="$(detect_ip)"; [[ "$got" == "$1" ]] || { printf 'Expected %s, got %s\n' "$1" "$got" >&2; exit 1; }; }
unset JUNPANEL_PUBLIC_IP
SSH_CONNECTION='1.1.1.1 1234 8.8.8.8 22'
TEST_ROUTE='1.1.1.1 via 10.0.0.1 dev eth0 src 9.9.9.9'
expect 8.8.8.8
SSH_CONNECTION='1.1.1.1 1234 10.0.0.2 22'
expect 9.9.9.9
TEST_ROUTE='1.1.1.1 via 10.0.0.1 dev eth0 src 10.0.0.2'
TEST_ADDRESSES=$'2: eth0 inet 10.0.0.2/24 scope global eth0\n3: eth1 inet 8.8.4.4/32 scope global eth1'
expect 8.8.4.4
TEST_ADDRESSES=$'2: eth0 inet 8.8.8.8/32 scope global eth0\n3: eth1 inet 9.9.9.9/32 scope global eth1'
if detect_ip >/dev/null; then printf 'Ambiguous public addresses accepted\n' >&2; exit 1; fi
TEST_ADDRESSES=$'2: eth0 inet 192.168.1.2/24 scope global eth0\n3: eth1 inet 203.0.113.10/24 scope global eth1'
if detect_ip >/dev/null; then printf 'Private/documentation address accepted\n' >&2; exit 1; fi
for invalid in 0.1.2.3 10.0.0.1 100.64.1.1 127.0.0.1 169.254.1.1 172.16.1.1 192.168.1.1 192.0.2.1 198.18.0.1 198.51.100.1 203.0.113.1 224.0.0.1 255.255.255.255 256.1.2.3 01.2.3.4; do
  if public_ip "$invalid"; then printf 'Non-public IPv4 accepted: %s\n' "$invalid" >&2; exit 1; fi
done
JUNPANEL_PUBLIC_IP=1.1.1.1
expect 1.1.1.1
JUNPANEL_PUBLIC_IP=10.0.0.1
if detect_ip >/dev/null; then printf 'Invalid override silently ignored\n' >&2; exit 1; fi
printf 'IPv4 detection: SSH, route, interface, ambiguity, non-public exclusions and override PASS\n'
