#!/bin/bash
# Writes the blocked list in the formats of the proxy clients:
#   build-lists.sh <list file> <output folder>
#
#   ir-blocked.txt        one domain per line, in rank order
#   ir-blocked-clash.txt  Clash Meta / Mihomo rule provider (behavior: domain, format: text)
#   ir-blocked-surge.txt  Surge, Shadowrocket and Stash rule set
#   ir-blocked.json       sing-box rule set source, compiled to ir-blocked.srs by the workflow
#
# Every format matches the domain and its subdomains, like geosite.dat does.
set -euo pipefail

list=$1
out=$2
mkdir -p "$out"

tr -d '\r' < "$list" | awk 'NF { print $1 }' > "$out/ir-blocked.txt"
sed 's/^/+./' "$out/ir-blocked.txt" > "$out/ir-blocked-clash.txt"
sed 's/^/DOMAIN-SUFFIX,/' "$out/ir-blocked.txt" > "$out/ir-blocked-surge.txt"

# Version 2 is read by sing-box 1.10 and newer
awk 'BEGIN { printf "{\"version\":2,\"rules\":[{\"domain_suffix\":[" }
     { printf "%s\"%s\"", (NR > 1 ? "," : ""), $0 }
     END { print "]}]}" }' "$out/ir-blocked.txt" > "$out/ir-blocked.json"
