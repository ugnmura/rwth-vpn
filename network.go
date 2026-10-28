package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

func validPort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}
func socksHandshake(conn net.Conn, host, port string) error {
	if len(host) == 0 || len(host) > 255 {
		return errors.New("SOCKS hostname must be 1 to 255 bytes")
	}
	if err := validPort(port); err != nil {
		return err
	}
	if err := writeAll(conn, []byte{5, 1, 0}); err != nil {
		return err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return err
	}
	if greeting != [2]byte{5, 0} {
		return errors.New("SOCKS proxy refused no-auth method")
	}
	p, _ := strconv.Atoi(port)
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	request = binary.BigEndian.AppendUint16(request, uint16(p))
	if err := writeAll(conn, request); err != nil {
		return err
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return err
	}
	if head[0] != 5 || head[2] != 0 {
		return errors.New("invalid SOCKS reply")
	}
	if head[1] != 0 {
		return fmt.Errorf("SOCKS connection refused (code %d)", head[1])
	}
	var n int
	switch head[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return err
		}
		n = int(size[0])
	default:
		return errors.New("invalid SOCKS address type")
	}
	_, err := io.CopyN(io.Discard, conn, int64(n+2))
	return err
}
func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
func relay(conn net.Conn, in io.Reader, out io.Writer) error {
	// Input may be a terminal/pipe that never closes. Return on remote EOF,
	// without waiting for that reader; the standalone helper then exits.
	var once sync.Once
	closeConn := func() { once.Do(func() { conn.Close() }) }
	defer closeConn()
	go func() {
		_, err := io.Copy(conn, in)
		if err != nil {
			closeConn()
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	_, err := io.Copy(out, conn)
	return err
}
func connect(host, port, proxy string, in io.Reader, out io.Writer) error {
	if err := validPort(port); err != nil {
		return err
	}
	addr := net.JoinHostPort(host, port)
	if proxy != "" {
		if err := validPort(proxy); err != nil {
			return err
		}
		addr = net.JoinHostPort("127.0.0.1", proxy)
	}
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if proxy != "" {
		if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return err
		}
		if err := socksHandshake(conn, host, port); err != nil {
			return err
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			return err
		}
	}
	return relay(conn, in, out)
}
func portOpen(port string) error {
	if err := validPort(port); err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
func gateway(ctx context.Context, host string) (string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	for _, ipv4 := range []bool{true, false} {
		for _, addr := range addrs {
			if (addr.IP.To4() != nil) != ipv4 {
				continue
			}
			d := net.Dialer{Timeout: 4 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), "443"))
			if err == nil {
				conn.Close()
				return addr.String(), nil
			}
		}
	}
	return "", fmt.Errorf("%s is unreachable on port 443", host)
}
