#!/usr/bin/env python3
"""ssh ProxyCommand: relay stdin/stdout to HOST:PORT, directly or via SOCKS5.

    connect.py HOST PORT                 direct TCP (system tunnel is up)
    connect.py HOST PORT SOCKS_PORT      through the local ocproxy SOCKS5 proxy

The hostname goes to the proxy unresolved, so the VPN's DNS resolves it.
Standard library only.
"""
import os
import selectors
import socket
import struct
import sys


def socks5(sock, host, port):
    sock.sendall(b"\x05\x01\x00")
    if sock.recv(2) != b"\x05\x00":
        sys.exit("socks: proxy refused no-auth method")
    h = host.encode()
    sock.sendall(b"\x05\x01\x00\x03" + bytes([len(h)]) + h + struct.pack(">H", port))
    head = recv_exact(sock, 4)
    if head[1] != 0:
        sys.exit(f"socks: connect to {host}:{port} failed (code {head[1]})")
    # Skip the bound address in the reply.
    atyp = head[3]
    n = {1: 4, 4: 16}.get(atyp)
    if n is None:
        n = recv_exact(sock, 1)[0]
    recv_exact(sock, n + 2)


def recv_exact(sock, n):
    buf = b""
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            sys.exit("socks: proxy closed the connection")
        buf += chunk
    return buf


def relay(sock):
    sel = selectors.DefaultSelector()
    stdin, stdout = sys.stdin.fileno(), sys.stdout.fileno()
    sel.register(stdin, selectors.EVENT_READ)
    sel.register(sock, selectors.EVENT_READ)
    open_ends = 2
    while open_ends:
        for key, _ in sel.select():
            if key.fileobj == stdin:
                data = os.read(stdin, 65536)
                if data:
                    sock.sendall(data)
                else:
                    sel.unregister(stdin)
                    sock.shutdown(socket.SHUT_WR)
                    open_ends -= 1
            else:
                data = sock.recv(65536)
                if data:
                    os.write(stdout, data)
                else:
                    sel.unregister(sock)
                    open_ends -= 1
                    return


def main():
    host, port = sys.argv[1], int(sys.argv[2])
    if len(sys.argv) > 3:
        sock = socket.create_connection(("127.0.0.1", int(sys.argv[3])), timeout=30)
        socks5(sock, host, port)
    else:
        sock = socket.create_connection((host, port), timeout=30)
    sock.settimeout(None)
    relay(sock)


if __name__ == "__main__":
    main()
