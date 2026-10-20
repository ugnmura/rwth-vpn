#!/usr/bin/env python3
"""RFC 6238 TOTP code for the secret on stdin (base32 or an otpauth:// URI).

Reads the secret from stdin so it never shows up in `ps`. Standard library only.

    totp.py           print the current code
    totp.py --fresh   a code valid for >= 5 s that no earlier --fresh call
                      returned for this secret (waits for the next one if needed)

Servers reject a code that was already accepted (replay), so two logins with
one token in one 30 s window (e.g. down and up again) must not share a code.
"""
import base64
import fcntl
import hashlib
import hmac
import os
import struct
import sys
import time
import urllib.parse

MIN_VALIDITY = 5


def parse(raw):
    """Return (key, digits, period, digest) from a base32 secret or otpauth URI."""
    raw = raw.strip()
    digits, period, digest = 6, 30, hashlib.sha1
    if raw.lower().startswith("otpauth://"):
        q = urllib.parse.parse_qs(urllib.parse.urlparse(raw).query)
        secret = q["secret"][0]
        digits = int(q.get("digits", ["6"])[0])
        period = int(q.get("period", ["30"])[0])
        digest = {"SHA1": hashlib.sha1, "SHA256": hashlib.sha256,
                  "SHA512": hashlib.sha512}[q.get("algorithm", ["SHA1"])[0].upper()]
    else:
        secret = raw
    secret = secret.replace(" ", "").replace("-", "").upper()
    if not secret:
        sys.exit("totp: empty secret")
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    return key, digits, period, digest


def code(key, digits, period, digest, now):
    mac = hmac.new(key, struct.pack(">Q", int(now) // period), digest).digest()
    off = mac[-1] & 0x0F
    n = struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF
    return str(n % 10 ** digits).zfill(digits)


def fresh_time(key, period):
    """Return a time in a step that is valid long enough and not yet used."""
    base = os.environ.get("XDG_RUNTIME_DIR") or os.path.expanduser("~/.cache")
    # Per token (VPN and HPC are separate), named by a hash, not the secret.
    tag = hashlib.sha256(key).hexdigest()[:12]
    path = os.path.join(base, "rwth-vpn", "totp-%s.last" % tag)
    os.makedirs(os.path.dirname(path), mode=0o700, exist_ok=True)
    with open(path, "a+") as f:
        # Held while waiting, so parallel callers get successive steps.
        fcntl.flock(f, fcntl.LOCK_EX)
        f.seek(0)
        last = f.read().strip()
        last = int(last) if last.isdigit() else -1
        while True:
            now = time.time()
            left = period - now % period
            if left >= MIN_VALIDITY and int(now // period) > last:
                break
            time.sleep(left + 0.2)
        f.seek(0)
        f.truncate()
        f.write(str(int(now // period)))
    return now


def main():
    key, digits, period, digest = parse(sys.stdin.read())
    now = time.time()
    if "--fresh" in sys.argv[1:]:
        now = fresh_time(key, period)
    print(code(key, digits, period, digest, now))


if __name__ == "__main__":
    main()
