#!/bin/sh
# Print tester's VPN/ssh process counts in container $1.
docker exec -u tester "$1" sh -c 'echo "openconnect=$(pgrep -u tester -c -x openconnect) ocproxy=$(pgrep -u tester -c -x ocproxy) masters=$(pgrep -u tester -c -f "^ssh -f -N -M") locks=$(ls -d ~/.cache/rwth-vpn/*.lock 2>/dev/null | wc -l) logins=$(grep -c "proxy up" ~/.cache/rwth-vpn/proxy.log 2>/dev/null)"'
