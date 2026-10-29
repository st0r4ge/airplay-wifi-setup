#!/bin/sh
# Reconnect to the target WiFi with NetworkManager. Uses WAC_NM_CONNECTION if
# set, otherwise the saved connection whose SSID equals WAC_SSID.
set -eu
CON=${WAC_NM_CONNECTION:-}
if [ -z "$CON" ]; then
  CON=$(nmcli -t -f NAME,TYPE con show | awk -F: '$2=="802-11-wireless"{print $1}' \
        | while IFS= read -r c; do
            [ "$(nmcli -g 802-11-wireless.ssid con show "$c")" = "$WAC_SSID" ] && { echo "$c"; break; }
          done)
fi
[ -n "$CON" ] || { echo "no saved NetworkManager connection for SSID '$WAC_SSID'" >&2; exit 1; }
nmcli con delete wac-setup-ap >/dev/null 2>&1 || true
echo "joining '$CON'"
nmcli con up "$CON" >/dev/null
