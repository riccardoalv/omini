//go:build linux

package netscan

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// sniffDHCP reads the DHCP requests that reach this server (UDP to port 67)
// with a packet socket: it only watches, so a DHCP server running here keeps
// its port. The kernel filter passes those packets alone. Needs raw sockets
// (root, or CAP_NET_RAW).
func sniffDHCP(ctx context.Context, packet func([]byte)) error {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, int(htons(unix.ETH_P_IP)))
	if err != nil {
		return fmt.Errorf("packet socket: %w", err)
	}
	defer unix.Close(fd)
	// With SOCK_DGRAM the packet starts at the IP header.
	prog, err := bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute{Off: 9, Size: 1},                           // protocol
		bpf.JumpIf{Cond: bpf.JumpNotEqual, Val: 17, SkipTrue: 6},    // UDP
		bpf.LoadAbsolute{Off: 6, Size: 2},                           // flags + fragment offset
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: 0x1fff, SkipTrue: 4}, // not a later fragment
		bpf.LoadMemShift{Off: 0},                                    // X = IP header length
		bpf.LoadIndirect{Off: 2, Size: 2},                           // UDP destination port
		bpf.JumpIf{Cond: bpf.JumpNotEqual, Val: 67, SkipTrue: 1},
		bpf.RetConstant{Val: 1500},
		bpf.RetConstant{Val: 0},
	})
	if err != nil {
		return err
	}
	filter := make([]unix.SockFilter, len(prog))
	for i, ins := range prog {
		filter[i] = unix.SockFilter{Code: ins.Op, Jt: ins.Jt, Jf: ins.Jf, K: ins.K}
	}
	if err := unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER,
		&unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}); err != nil {
		return fmt.Errorf("filter: %w", err)
	}
	// Wake up every second to notice ctx.
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 1}); err != nil {
		return err
	}
	buf := make([]byte, 1500)
	for ctx.Err() == nil {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		switch {
		case errors.Is(err, unix.EAGAIN), errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return err
		}
		packet(append([]byte(nil), buf[:n]...))
	}
	return nil
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }
