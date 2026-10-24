#!/bin/sh
docker rm -f rwtest-client rwtest-root rwtest-ocserv rwtest-sshd 2>/dev/null
docker network rm rwtest-inside rwtest-outside 2>/dev/null
true
