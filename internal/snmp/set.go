package snmp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

const oidIfAdminStatus = oidIfTable + ".7" // up(1), down(2)

// ErrNoWrite is returned when the agent refuses to write: the credential is
// read-only, or its view does not include the object.
var ErrNoWrite = errors.New("the SNMP agent refused to write (read-only community or user)")

// SetPortEnabled enables or disables a port (IF-MIB ifAdminStatus), found by
// the name Collect gives it, and reads it back to check the agent took it.
// SNMP has no standard "save": whether the change survives a reboot is up to
// the device (many switches save on their own).
func SetPortEnabled(ctx context.Context, t Target, port string, enabled bool) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	idx, err := portIndex(c, port)
	if err != nil {
		return err
	}
	value := 2
	if enabled {
		value = 1
	}
	oid := oidIfAdminStatus + "." + idx
	res, err := c.g.Set([]gosnmp.SnmpPDU{{Name: oid, Type: gosnmp.Integer, Value: value}})
	if err != nil {
		return err
	}
	switch res.Error {
	case gosnmp.NoError:
	case gosnmp.NoAccess, gosnmp.AuthorizationError, gosnmp.NotWritable, gosnmp.ReadOnly, gosnmp.NoSuchName:
		return ErrNoWrite
	default:
		return fmt.Errorf("the SNMP agent refused the change: %s", res.Error)
	}
	got, err := c.get(oid)
	if err != nil {
		return err
	}
	if v, ok := pduInt(got[oid]); !ok || v != int64(value) {
		return fmt.Errorf("the agent did not apply the change to %s", port)
	}
	return nil
}

// portIndex is the ifIndex of the port Collect names port (ifName, else
// ifDescr, else "if<index>"). A name several interfaces share is refused:
// changing one of them at random could cut the wrong port.
func portIndex(c *client, port string) (string, error) {
	ifRows, err := c.walk(oidIfTable + ".2")
	if err != nil {
		return "", err
	}
	xRows, _ := c.walk(oidIfXTable + ".1") // ifName (optional)
	var found []int
	for idx, p := range ifRows {
		name := pduString(xRows[idx])
		if name == "" {
			name = pduString(p)
		}
		if name == "" {
			name = "if" + idx
		}
		if n, err := strconv.Atoi(idx); err == nil && name == port {
			found = append(found, n)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("the device has no port %q", port)
	case 1:
		return strconv.Itoa(found[0]), nil
	}
	slices.Sort(found)
	return "", fmt.Errorf("the device has %d ports named %q (ifIndex %s): change it in the device's own interface", len(found), port, strings.Trim(fmt.Sprint(found), "[]"))
}
