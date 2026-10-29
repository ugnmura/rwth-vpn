package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSOCKSFragmentedReply(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	result := make(chan error, 1)
	go func() {
		buf := make([]byte, 3)
		if _, err := io.ReadFull(server, buf); err != nil {
			result <- err
			return
		}
		if !bytes.Equal(buf, []byte{5, 1, 0}) {
			result <- io.ErrUnexpectedEOF
			return
		}
		for _, b := range []byte{5, 0} {
			if _, err := server.Write([]byte{b}); err != nil {
				result <- err
				return
			}
		}
		header := make([]byte, 5)
		if _, err := io.ReadFull(server, header); err != nil {
			result <- err
			return
		}
		target := make([]byte, int(header[4])+2)
		if _, err := io.ReadFull(server, target); err != nil {
			result <- err
			return
		}
		if string(target[:len(target)-2]) != "internal.rwth.test" {
			result <- io.ErrUnexpectedEOF
			return
		}
		for _, b := range []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 22} {
			if _, err := server.Write([]byte{b}); err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	if err := socksHandshake(client, "internal.rwth.test", "22"); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestSOCKSRejectsInvalidTargets(t *testing.T) {
	for _, host := range []string{"", strings.Repeat("x", 256)} {
		if err := socksHandshake(nil, host, "22"); err == nil {
			t.Fatal("accepted invalid hostname")
		}
	}
	if err := socksHandshake(nil, "host", "70000"); err == nil {
		t.Fatal("accepted invalid port")
	}
}

func TestSOCKSRejectsAuthentication(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		_, _ = io.CopyN(io.Discard, server, 3)
		_, _ = server.Write([]byte{5, 255})
	}()
	if err := socksHandshake(client, "host", "22"); err == nil {
		t.Fatal("accepted authentication rejection")
	}
}

func TestRelayWritesAllOutput(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	payload := bytes.Repeat([]byte("ssh-data"), 20000)
	go func() { _, _ = server.Write(payload); _ = server.Close() }()
	var output bytes.Buffer
	if err := relay(client, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), payload) {
		t.Fatal("relay lost output")
	}
}

func TestReachableAddress(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	address, err := reachableAddress(context.Background(), net.JoinHostPort("localhost", port))
	if err != nil || address != "127.0.0.1" {
		t.Fatalf("resolved gateway: %s %v", address, err)
	}
}

func TestReachableAddressHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := reachableAddress(ctx, "localhost:443")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
