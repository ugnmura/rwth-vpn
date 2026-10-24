#!/bin/sh
set -e
. /lab/lab.env
ssh-keygen -A >/dev/null
mkdir -p /run/sshd
OTP_FILE=/etc/users.oath
# otpuser: a login name containing "otp" must not confuse askpass.
for u in "$VPN_USER" otpuser; do
    id "$u" >/dev/null 2>&1 || useradd -m -s /bin/bash "$u"
    printf "%s:%s\n" "$u" "$(cat /lab/hpc-password.txt)" | chpasswd
    grep -q "[[:space:]]$u[[:space:]]" "$OTP_FILE" 2>/dev/null ||
        echo "HOTP/T30/6 $u - $HPC_TOTP_HEX" >> "$OTP_FILE"
done
chmod 600 "$OTP_FILE"
grep -q pam_oath /etc/pam.d/sshd ||
    sed -i "/^@include common-auth/a auth required pam_oath.so usersfile=$OTP_FILE window=2 digits=6" /etc/pam.d/sshd
cat > /etc/ssh/sshd_config.d/lab.conf <<EOC
UsePAM yes
KbdInteractiveAuthentication yes
PasswordAuthentication no
PubkeyAuthentication no
AuthenticationMethods keyboard-interactive
LogLevel VERBOSE
EOC
exec /usr/sbin/sshd -D -e
