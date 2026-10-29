#!/bin/bash
# Run the rwth-vpn lab test cases a-l against a lab built by setup.sh.
#   ./tests.sh [a b c ...]     (default: all)
# Prints evidence and a PASS/FAIL line per case; summary at the end.
set -u
LAB=$(cd "$(dirname "$0")" && pwd)
. "$LAB/lab.env"
C=rwtest-client R=rwtest-root
RESULTS=()

u() { docker exec -u tester -w /home/tester "$C" bash -c "$1" 2>&1; }
r() { docker exec -w /root "$R" bash -c "$1" 2>&1; }
verdict() { RESULTS+=("$1 $2"); echo ">>> $1: $2"; echo; }
reset() { "$LAB/reset-session.sh" "$C" >/dev/null 2>&1; }
counts() { "$LAB/count.sh" "$C"; }

t_a() {
    echo "== a. secrets, totp"
    local out ok=1
    docker cp "$LAB/password.txt" "$C:/tmp/pw"; docker exec "$C" chown tester /tmp/pw
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; pw=$(cat /tmp/pw); rm /tmp/pw
        [ "$(secret_get vpn_password)" = "$pw" ] && echo "roundtrip OK [$pw]" || echo "roundtrip FAIL"
        secret_set scratch "$pw"; [ "$(secret_get scratch)" = "$pw" ] && echo "set/get OK"; secret_set scratch ""
        secret_get scratch >/dev/null || echo "delete OK"
        echo "mode $(stat -c %a ~/.config/rwth-vpn/secrets)"
        echo "vpn  totp=$(rwth-vpn totp) oathtool=$(oathtool --totp -b '"$TOTP_SECRET"')"
        echo "hpc  totp=$(rwth-vpn totp hpc) oathtool=$(oathtool --totp -b '"$HPC_TOTP_SECRET"')"
        echo "uri  totp=$(echo "otpauth://totp/x?secret='"$TOTP_SECRET"'&digits=6" | rwth-vpn _totp)"
        rwth-vpn test-login
        rwth-vpn check')
    echo "$out"
    grep -q "roundtrip OK" <<<"$out" && grep -q "set/get OK" <<<"$out" && grep -q "delete OK" <<<"$out" || ok=0
    grep -q "mode 600" <<<"$out" || ok=0
    awk '/totp=/{split($2,a,"=");split($3,b,"="); if (a[2]!=b[2]) exit 1}' <<<"$(grep -E '^(vpn|hpc) ' <<<"$out")" || ok=0
    grep -q "ok: VPN login works" <<<"$out" || ok=0
    grep -q "ok: rwtest-ocserv (.*) offers the login form" <<<"$out" || ok=0
    [ $ok = 1 ] && verdict a PASS || verdict a FAIL
}

t_b() {
    echo "== b. cold start, then reuse"
    reset
    local out
    out=$(u 's=$(date +%s%N); h=$(ssh -o BatchMode=yes claix hostname </dev/null); rc=$?
        echo "cold: rc=$rc host=$h $(( ($(date +%s%N)-s)/1000000 )) ms"
        s=$(date +%s%N); h=$(ssh -o BatchMode=yes claix hostname </dev/null); rc=$?
        echo "warm: rc=$rc host=$h $(( ($(date +%s%N)-s)/1000000 )) ms"')
    echo "$out"; counts
    grep -q "cold: rc=0 host=claix-test-sshd" <<<"$out" && grep -q "warm: rc=0 host=claix-test-sshd" <<<"$out" &&
        counts | grep -q "logins=1" && verdict b PASS || verdict b FAIL
}

t_c() {
    echo "== c. 5 parallel cold calls"
    reset
    local out
    out=$(u 's=$(date +%s)
        for i in 1 2 3 4 5; do ( o=$(ssh -o BatchMode=yes claix "echo call'"\$i"'" </dev/null 2>&1); echo "rc=$? $o $(( $(date +%s)-s ))s" ) & done; wait')
    echo "$out"; counts
    [ "$(grep -c '^rc=0' <<<"$out")" = 5 ] &&
        [ "$(grep -c 'call[1-5] [0-9]*s$' <<<"$out")" = 5 ] &&
        counts | grep -q "openconnect=1 ocproxy=1 masters=1 locks=0 logins=1" && verdict c PASS || verdict c FAIL
}

t_d() {
    echo "== d. scp, rsync"
    local out
    out=$(u 'rm -rf big.* tree tree.back; head -c 3000000 /dev/urandom >big.bin
        mkdir -p tree/sub; echo a >tree/a.txt; echo b >tree/sub/b.txt
        scp -o BatchMode=yes big.bin claix:big.bin </dev/null && scp -o BatchMode=yes claix:big.bin big.back </dev/null &&
            cmp big.bin big.back && echo "scp OK"
        rsync -a -e "ssh -o BatchMode=yes" tree/ claix:tree/ </dev/null && rsync -a -e ssh claix:tree/ tree.back/ </dev/null &&
            diff -r tree tree.back && echo "rsync OK"')
    echo "$out"
    grep -q "scp OK" <<<"$out" && grep -q "rsync OK" <<<"$out" && verdict d PASS || verdict d FAIL
}

t_e() {
    echo "== e. status, down, recovery"
    local out
    out=$(u 'ssh -o BatchMode=yes claix true </dev/null
        echo "-- status"; rwth-vpn status
        echo "-- down"; rwth-vpn down
        echo "after down: oc=$(pgrep -c -x openconnect) masters=$(pgrep -c -f "^ssh -f -N -M")"
        s=$(date +%s); h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "recover: rc=$? host=$h $(( $(date +%s)-s ))s"
        echo "-- simulated drop (kill -9 openconnect)"; pkill -9 -x openconnect; sleep 1
        s=$(date +%s); h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "recover2: rc=$? host=$h $(( $(date +%s)-s ))s"')
    echo "$out"; counts
    grep -q "ssh proxy session: up" <<<"$out" && grep -q "ssh proxy session stopped" <<<"$out" &&
        grep -q "after down: oc=0 masters=0" <<<"$out" && grep -q "recover: rc=0 host=claix" <<<"$out" &&
        grep -q "recover2: rc=0 host=claix" <<<"$out" && verdict e PASS || verdict e FAIL
}

t_f() {
    echo "== f. wrong secrets fail cleanly"
    local out ok=1
    reset
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; good=$(secret_get totp_secret)
        secret_set totp_secret GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
        s=$(date +%s); timeout 300 ssh -o BatchMode=yes claix hostname </dev/null; echo "wrong VPN TOTP: rc=$? $(( $(date +%s)-s ))s"
        echo "left: oc=$(pgrep -c -x openconnect) masters=$(pgrep -c -f "^ssh -f -N -M") hooks=$(pgrep -fc "[r]wth-vpn _ssh") locks=$(ls -d ~/.cache/rwth-vpn/*.lock 2>/dev/null | wc -l)"
        secret_set totp_secret "$good"; rm -f ~/.cache/rwth-vpn/*-login-failed  # as "rwth-vpn secrets" does')
    echo "$out"
    grep -q "wrong VPN TOTP: rc=255" <<<"$out" && grep -q "left: oc=0 masters=0 hooks=0 locks=0" <<<"$out" || ok=0
    reset
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; good=$(secret_get hpc_totp_secret)
        secret_set hpc_totp_secret GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
        s=$(date +%s); timeout 300 ssh -o BatchMode=yes claix hostname </dev/null; echo "wrong HPC TOTP: rc=$? $(( $(date +%s)-s ))s"
        echo "left: masters=$(pgrep -c -f "^ssh -f -N -M") hooks=$(pgrep -fc "[r]wth-vpn _ssh") locks=$(ls -d ~/.cache/rwth-vpn/*.lock 2>/dev/null | wc -l)"
        secret_set hpc_totp_secret "$good"; rm -f ~/.cache/rwth-vpn/*-login-failed
        s=$(date +%s); h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "fixed again: rc=$? host=$h $(( $(date +%s)-s ))s"')
    echo "$out"
    docker logs --since 10m rwtest-sshd 2>&1 | grep -c "Failed keyboard-interactive" | sed 's/^/sshd failed kbd-int attempts (10 min): /'
    grep -q "wrong HPC TOTP: rc=255" <<<"$out" && grep -q "left: masters=0 hooks=0 locks=0" <<<"$out" &&
        grep -q "fixed again: rc=0" <<<"$out" || ok=0
    [ $ok = 1 ] && verdict f PASS || verdict f FAIL
}

t_g() {
    echo "== g. root system tunnel"
    local out
    # Root's client shares the VPN token with tester's (another "machine"), and
    # client-side replay state is per machine: start in a step tester didn't use.
    sleep $((31 - $(date +%s) % 30))
    out=$(r 'rwth-vpn down >/dev/null 2>&1
        echo "-- up"; s=$(date +%s); rwth-vpn up </dev/null; echo "up rc=$? $(( $(date +%s)-s ))s"
        echo "-- status"; rwth-vpn status; echo "status rc=$?"
        ip route | grep -E "^default"
        timeout 5 nc -z 172.30.99.10 22 && echo "sshd reachable directly"
        h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "ssh via system tunnel: rc=$? host=$h"
        pgrep -x ocproxy >/dev/null || echo "no ocproxy used"
        echo "-- down"; rwth-vpn down; rwth-vpn status; echo "status rc=$?"
        ip route | grep -E "^default"; ip link show tun-rwth >/dev/null 2>&1 || echo "tun-rwth gone"
        h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "ssh after system down: rc=$? host=$h"
        rwth-vpn down >/dev/null')
    sleep $((31 - $(date +%s) % 30))  # leave the step root's logins used
    echo "$out"
    docker logs --since 5m rwtest-ocserv 2>&1 | grep "auth complete" | tail -n 3
    grep -q "up rc=0" <<<"$out" && grep -q "connected (full tunnel)" <<<"$out" &&
        grep -q "ssh via system tunnel: rc=0 host=claix" <<<"$out" && grep -q "tun-rwth gone" <<<"$out" &&
        grep -q "ssh after system down: rc=0" <<<"$out" && verdict g PASS || verdict g FAIL
}

t_h() {
    echo "== h. other hosts unaffected"
    local out
    out=$(u 'echo "somewhere:"; ssh -G somewhere </dev/null | grep -iE "^(proxycommand|controlmaster|controlpath) "
        echo "github.com rwth lines: $(ssh -G github.com </dev/null | grep -ci rwth)"
        echo "claix:"; ssh -G claix </dev/null | grep -iE "^(hostname|proxycommand|controlmaster) "')
    echo "$out"
    ! sed -n '/somewhere:/,/github/p' <<<"$out" | grep -qi "proxycommand\|controlmaster auto" &&
        grep -q "github.com rwth lines: 0" <<<"$out" && grep -q "proxycommand.*_ssh-proxy" <<<"$out" &&
        verdict h PASS || verdict h FAIL
}

oc_fail() { docker logs rwtest-ocserv 2>&1 | grep -c "OTP auth failed"; }
sshd_fail() { docker logs rwtest-sshd 2>&1 | grep -c "Failed keyboard-interactive"; }

t_i() {
    echo "== i. wrong VPN TOTP: one attempt, then paused"
    local out n0 n1 n2 ok=1
    reset
    u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; secret_get totp_secret >/tmp/good.vpn
       secret_set totp_secret GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ; rm -f ~/.cache/rwth-vpn/*-login-failed' >/dev/null
    n0=$(oc_fail)
    out=$(u 's=$(date +%s); ssh -o BatchMode=yes claix true </dev/null; echo "first: rc=$? $(( $(date +%s)-s ))s"')
    echo "$out"; n1=$(oc_fail); echo "ocserv failed logins for the first call: $((n1 - n0))"
    out=$(u 's=$(date +%s); for k in 1 2 3 4 5; do ssh -o BatchMode=yes claix true </dev/null; echo "rc=$?"; done 2>&1 | sort | uniq -c
        echo "5 calls took $(( $(date +%s)-s ))s"')
    echo "$out"; n2=$(oc_fail); echo "ocserv failed logins for the next 5 calls: $((n2 - n1))"
    [ $((n1 - n0)) = 1 ] && [ $((n2 - n1)) = 0 ] && grep -q "paused" <<<"$out" || ok=0
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; secret_set totp_secret "$(cat /tmp/good.vpn)"; rm /tmp/good.vpn
        rm -f ~/.cache/rwth-vpn/vpn-login-failed
        h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "after fix: rc=$? host=$h"')
    echo "$out"; grep -q "after fix: rc=0 host=claix" <<<"$out" || ok=0
    [ $ok = 1 ] && verdict i PASS || verdict i FAIL
}

t_j() {
    echo "== j. wrong HPC TOTP: one attempt, then paused"
    local out n0 n1 n2 ok=1
    reset
    u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; secret_get hpc_totp_secret >/tmp/good.hpc
       secret_set hpc_totp_secret GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ; rm -f ~/.cache/rwth-vpn/*-login-failed' >/dev/null
    n0=$(sshd_fail)
    out=$(u 's=$(date +%s); ssh -o BatchMode=yes claix true </dev/null; echo "first: rc=$? $(( $(date +%s)-s ))s"')
    echo "$out"; sleep 1; n1=$(sshd_fail); echo "sshd failed auths for the first call: $((n1 - n0))"
    out=$(u 's=$(date +%s); for k in 1 2 3; do ssh -o BatchMode=yes claix true </dev/null; echo "rc=$?"; done 2>&1 | sort | uniq -c
        echo "3 calls took $(( $(date +%s)-s ))s"')
    echo "$out"; sleep 1; n2=$(sshd_fail); echo "sshd failed auths for the next 3 calls: $((n2 - n1))"
    [ $((n1 - n0)) = 1 ] && [ $((n2 - n1)) = 0 ] && grep -q "paused" <<<"$out" || ok=0
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; secret_set hpc_totp_secret "$(cat /tmp/good.hpc)"; rm /tmp/good.hpc
        rm -f ~/.cache/rwth-vpn/hpc-login-failed
        h=$(ssh -o BatchMode=yes claix hostname </dev/null); echo "after fix: rc=$? host=$h"')
    echo "$out"; grep -q "after fix: rc=0 host=claix" <<<"$out" || ok=0
    [ $ok = 1 ] && verdict j PASS || verdict j FAIL
}

t_k() {
    echo "== k. HPC username containing otp"
    local out
    reset
    out=$(u 'secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }; secret_set hpc_user otpuser
        rwth-vpn ssh-setup >/dev/null; sed -i "s/HostName login23-1.hpc.itc.rwth-aachen.de/HostName '"$SSHD_IP"'/" ~/.ssh/rwth-vpn.conf
        grep "User " ~/.ssh/rwth-vpn.conf
        h=$(ssh -o BatchMode=yes claix whoami </dev/null); echo "otpuser via config: rc=$? whoami=$h"
        echo "askpass log:"; grep -i "unrecognised" ~/.cache/rwth-vpn/ssh.log
        secret_set hpc_user ""; rwth-vpn ssh-setup >/dev/null
        sed -i "s/HostName login23-1.hpc.itc.rwth-aachen.de/HostName '"$SSHD_IP"'/" ~/.ssh/rwth-vpn.conf
        ssh -O exit claix 2>/dev/null; true')
    echo "$out"
    grep -q "otpuser via config: rc=0 whoami=otpuser" <<<"$out" && verdict k PASS || verdict k FAIL
}

t_l() {
    echo "== l. killed full tunnel restores routing and DNS"
    local out
    docker exec rwtest-ocserv sh -c 'cp /etc/ocserv/groups/full /tmp/full-before-crash-test; printf "\ndns = 192.168.99.1\n" >> /etc/ocserv/groups/full'
    # Root and tester share a token: use a step tester has not consumed.
    sleep $((31 - $(date +%s) % 30))
    out=$(r 'set -e
        umask 077
        rwth-vpn down >/dev/null
        ip -4 route show default >/tmp/default-before-crash
        cp /etc/resolv.conf /tmp/resolv-before-crash
        rwth-vpn up </dev/null
        test "$(stat -c %a /var/run/rwth-vpn/disconnect.env)" = 600
        test "$(stat -c %a /var/run/rwth-vpn/openconnect.pid)" = 644
        test "$(stat -c %a /var/run/rwth-vpn/state)" = 644
        grep -q "nameserver 192.168.99.1" /etc/resolv.conf
        ip -6 rule add pref 5281 lookup 7338
        kill -KILL "$(cat /var/run/rwth-vpn/openconnect.pid)"
        sleep 1
        rwth-vpn down
        ip -4 route show default >/tmp/default-after-crash
        cmp /tmp/default-before-crash /tmp/default-after-crash
        cmp /tmp/resolv-before-crash /etc/resolv.conf
        ! ip -6 rule | grep -q "5281:"
        test ! -e /var/run/rwth-vpn/disconnect.env
        test ! -e /var/run/rwth-vpn/state
        rwth-vpn down
        echo "crash recovery OK"')
    echo "$out"
    docker exec rwtest-ocserv sh -c 'mv /tmp/full-before-crash-test /etc/ocserv/groups/full'
    grep -q "crash recovery OK" <<<"$out" && verdict l PASS || verdict l FAIL
}

for t in ${@:-a b c d e f g h i j k l}; do "t_$t"; done
echo "== summary"; printf '%s\n' "${RESULTS[@]}"
for result in "${RESULTS[@]}"; do
    case "$result" in *" FAIL") exit 1;; esac
done
