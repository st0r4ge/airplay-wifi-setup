#!/bin/sh
# Join the accessory's setup AP with NetworkManager. Gets WAC_AP_PATTERN and
# WAC_IFACE from airplay-wifi-setup. Connects by BSSID because setup SSIDs may
# contain non-printable bytes that break matching by name.
set -eu
IFACE=${WAC_IFACE:-$(nmcli -t -f DEVICE,TYPE dev | awk -F: '$2=="wifi"{print $1; exit}')}
nmcli dev wifi rescan ifname "$IFACE" >/dev/null 2>&1 || true
sleep 4
BSSID=$(nmcli -t -f SSID,BSSID dev wifi list ifname "$IFACE" | sed 's/\\:/-/g' \
        | awk -F: -v re="$WAC_AP_PATTERN" 'tolower($1) ~ re {print $2; exit}' | tr '-' ':')
[ -n "$BSSID" ] || { echo "no setup AP matching /$WAC_AP_PATTERN/ visible" >&2; exit 1; }
echo "joining setup AP $BSSID on $IFACE"
nmcli con delete wac-setup-ap >/dev/null 2>&1 || true
nmcli dev wifi connect "$BSSID" ifname "$IFACE" name wac-setup-ap >/dev/null
