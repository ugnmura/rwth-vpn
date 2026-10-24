#!/bin/sh
# Kill tester's ssh master and proxy session in container $1 (cold state).
docker exec -u tester -w /home/tester "$1" bash -c '
ssh -O exit claix >/dev/null 2>&1; rwth-vpn down >/dev/null 2>&1
pkill -u tester -f "[s]sh -f -N -M" ; pkill -u tester openconnect; sleep 0.5
rm -f ~/.ssh/rwth-vpn-cm/* ~/.cache/rwth-vpn/*.log; ls -A ~/.cache/rwth-vpn ~/.ssh/rwth-vpn-cm'
