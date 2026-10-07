//go:build !unix

package netscan

import "syscall"

func reusePort(_, _ string, _ syscall.RawConn) error { return nil }
