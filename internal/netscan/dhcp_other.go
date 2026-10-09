//go:build !linux

package netscan

import (
	"context"
	"errors"
)

// sniffDHCP needs Linux packet sockets.
func sniffDHCP(context.Context, func([]byte)) error {
	return errors.New("watching DHCP requests needs Linux")
}
