package snmp

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gosnmp/gosnmp"
	"go.yaml.in/yaml/v3"

	"github.com/riccardoalv/omini/internal/model"
)

// Profile adds what the standard MIBs do not have for a kind of device:
// vendor, role and model, and vendor OIDs for CPU, memory, temperatures and
// firmware. Profiles are YAML files: the ones shipped in profiles/, plus the
// user's in <data>/profiles (a profile with the same id replaces the shipped
// one). Every profile that matches a device is applied, by ascending
// priority: later ones win.
type Profile struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Priority int    `yaml:"priority"`
	Match    Match  `yaml:"match"`

	Vendor string `yaml:"vendor"`
	Role   string `yaml:"role"` // router | switch | ap | firewall | server

	Model     *Value `yaml:"model"`
	OSVersion *Value `yaml:"os_version"`
	Serial    *Value `yaml:"serial"`
	CPUPct    *Value `yaml:"cpu_pct"`
	MemPct    *Value `yaml:"mem_pct"`
	SwapPct   *Value `yaml:"swap_pct"`
	// LoadAvg: 1, 5 and 15 minutes.
	LoadAvg      []Value       `yaml:"load_avg"`
	Temperatures []Temperature `yaml:"temperatures"`
	Firmware     *Firmware     `yaml:"firmware"`
	// FdbPerVLAN: the MAC table is kept per VLAN, each read on its own (Cisco
	// IOS: BRIDGE-MIB through community@vlan, or the v3 context vlan-N).
	FdbPerVLAN *FdbPerVLAN `yaml:"fdb_per_vlan"`

	source string // file it came from, for messages
}

// FdbPerVLAN tells how to read a MAC table kept per VLAN.
type FdbPerVLAN struct {
	// VLANs is a column whose rows end with a VLAN id (e.g. vtpVlanState).
	VLANs string `yaml:"vlans"`
	// Community for SNMP v1/v2c, with {community} and {vlan}: "{community}@{vlan}".
	Community string `yaml:"community"`
	// Context for SNMP v3, with {vlan}: "vlan-{vlan}".
	Context string `yaml:"context"`
}

// Match tells which devices a profile is for. Every key given must match;
// within a list, any entry may.
type Match struct {
	// SysObjectID prefixes, e.g. "1.3.6.1.4.1.14988" (MikroTik's enterprise).
	SysObjectID []string `yaml:"sys_object_id"`
	// SysDescr is a regular expression on sysDescr (case-insensitive).
	SysDescr string `yaml:"sys_descr"`
	// OIDExists: the device answers one of these OIDs (a vendor MIB is there).
	OIDExists []string `yaml:"oid_exists"`
}

// Value reads a number or text from an OID.
type Value struct {
	OID string `yaml:"oid"`
	// Walk reads a column (every row) and combines it: avg, max, min, sum or
	// first. Without it, OID is a scalar (…​.0).
	Walk string `yaml:"walk"`
	// Scale multiplies the number (0.1 for tenths of a degree).
	Scale float64 `yaml:"scale"`
	// Invert gives 100 - value (an idle percentage becomes a busy one).
	Invert bool `yaml:"invert"`
	// Used/Free with Total give a percentage: used ÷ total, or 1 - free ÷ total
	// (Free may list several OIDs, added up: available + buffers + cached).
	// Used and Free without Total: used ÷ (used + free).
	Used  *Value  `yaml:"used"`
	Free  []Value `yaml:"free"`
	Total *Value  `yaml:"total"`
	// Map turns a value into another ("1": "true").
	Map map[string]string `yaml:"map"`
}

// Temperature is one sensor read from an OID.
type Temperature struct {
	Sensor string `yaml:"sensor"`
	Kind   string `yaml:"kind"` // cpu | disk | board | other
	Value  `yaml:",inline"`
}

// Firmware: what the device knows about its own updates (Omini never asks
// it to check).
type Firmware struct {
	Current         *Value `yaml:"current"`
	Latest          *Value `yaml:"latest"`
	UpdateAvailable *Value `yaml:"update_available"` // mapped to "true" / "false"
}

//go:embed profiles/*.yaml
var shipped embed.FS

var (
	profilesMu sync.RWMutex
	profiles   []Profile
)

func init() {
	list, err := readProfiles(shipped, "profiles")
	if err != nil {
		panic(fmt.Sprintf("shipped SNMP profiles: %v", err))
	}
	profiles = list
}

// LoadProfiles reads the user's profiles from dirs (missing dirs are fine)
// on top of the shipped ones, and returns how many are in use.
func LoadProfiles(dirs ...string) (int, error) {
	list, err := readProfiles(shipped, "profiles")
	if err != nil {
		return 0, err
	}
	byID := map[string]int{}
	for i, p := range list {
		byID[p.ID] = i
	}
	var errs []error
	for _, dir := range dirs {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		own, err := readProfiles(os.DirFS(dir), ".")
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))
			continue
		}
		for _, p := range own {
			if i, ok := byID[p.ID]; ok {
				list[i] = p
				continue
			}
			byID[p.ID] = len(list)
			list = append(list, p)
		}
	}
	sortProfiles(list)
	profilesMu.Lock()
	profiles = list
	profilesMu.Unlock()
	return len(list), errors.Join(errs...)
}

// Profiles returns the profiles in use.
func Profiles() []Profile {
	profilesMu.RLock()
	defer profilesMu.RUnlock()
	return append([]Profile(nil), profiles...)
}

func sortProfiles(list []Profile) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Priority < list[j].Priority })
}

func readProfiles(fsys fs.FS, dir string) ([]Profile, error) {
	files, err := fs.Glob(fsys, filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []Profile
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		p, err := ParseProfile(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		p.source = f
		out = append(out, p)
	}
	sortProfiles(out)
	return out, nil
}

// ParseProfile reads and checks one profile.
func ParseProfile(raw []byte) (Profile, error) {
	var p Profile
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	if p.ID == "" {
		return p, errors.New("id is required")
	}
	m := p.Match
	if len(m.SysObjectID) == 0 && m.SysDescr == "" && len(m.OIDExists) == 0 {
		return p, errors.New("match needs sys_object_id, sys_descr or oid_exists")
	}
	if m.SysDescr != "" {
		if _, err := regexp.Compile("(?i)" + m.SysDescr); err != nil {
			return p, fmt.Errorf("sys_descr: %w", err)
		}
	}
	switch p.Role {
	case "", "router", "switch", "ap", "firewall", "server":
	default:
		return p, fmt.Errorf("unknown role %q", p.Role)
	}
	if f := p.FdbPerVLAN; f != nil {
		if f.VLANs == "" || (f.Community == "" && f.Context == "") {
			return p, errors.New("fdb_per_vlan needs vlans and a community or a context")
		}
	}
	for _, t := range p.Temperatures {
		switch t.Kind {
		case "", "cpu", "disk", "board", "other":
		default:
			return p, fmt.Errorf("temperature %q: unknown kind %q", t.Sensor, t.Kind)
		}
	}
	return p, nil
}

// matches reports whether the profile is for this device.
func (p Profile) matches(c *client, sysObjectID, sysDescr string) bool {
	m := p.Match
	if len(m.SysObjectID) > 0 {
		oid := strings.TrimPrefix(sysObjectID, ".")
		ok := false
		for _, prefix := range m.SysObjectID {
			prefix = strings.TrimPrefix(prefix, ".")
			ok = ok || oid == prefix || strings.HasPrefix(oid, prefix+".")
		}
		if !ok {
			return false
		}
	}
	if m.SysDescr != "" && !regexp.MustCompile("(?i)"+m.SysDescr).MatchString(sysDescr) {
		return false
	}
	if len(m.OIDExists) > 0 {
		got, err := c.get(m.OIDExists...)
		if err != nil || len(got) == 0 {
			return false
		}
	}
	return true
}

// applyProfiles fills a device from every profile that matches it.
func applyProfiles(c *client, d *model.Device, sysObjectID, sysDescr string) (used []string, fdb *FdbPerVLAN) {
	for _, p := range Profiles() {
		if !p.matches(c, sysObjectID, sysDescr) {
			continue
		}
		used = append(used, p.ID)
		p.apply(c, d)
		if p.FdbPerVLAN != nil {
			fdb = p.FdbPerVLAN
		}
	}
	return used, fdb
}

func (p Profile) apply(c *client, d *model.Device) {
	r := reader{c: c}
	if p.Vendor != "" {
		d.Vendor = model.Ptr(p.Vendor)
	}
	if p.Role != "" {
		d.Role = model.Ptr(model.DeviceRole(p.Role))
	}
	setText := func(dst **string, v *Value) {
		if s, ok := r.text(v); ok {
			*dst = model.Ptr(s)
		}
	}
	setText(&d.Model, p.Model)
	setText(&d.OSVersion, p.OSVersion)
	setText(&d.Serial, p.Serial)
	setPct := func(dst **float64, v *Value) {
		if f, ok := r.number(v); ok {
			*dst = model.Ptr(math.Round(math.Max(0, math.Min(100, f))*10) / 10)
		}
	}
	setPct(&d.CPUPct, p.CPUPct)
	setPct(&d.MemPct, p.MemPct)
	setPct(&d.SwapPct, p.SwapPct)
	if len(p.LoadAvg) > 0 {
		var loads []float64
		for i := range p.LoadAvg {
			f, ok := r.number(&p.LoadAvg[i])
			if !ok {
				loads = nil
				break
			}
			loads = append(loads, f)
		}
		if loads != nil {
			d.LoadAvg = loads
		}
	}
	for i := range p.Temperatures {
		t := p.Temperatures[i]
		f, ok := r.number(&t.Value)
		if !ok || f <= -50 || f >= 200 { // sensors report 0 or junk when absent
			continue
		}
		temp := model.Temperature{Sensor: t.Sensor, Celsius: math.Round(f*10) / 10}
		if t.Kind != "" {
			temp.Kind = model.Ptr(model.TemperatureKind(t.Kind))
		}
		d.Temperatures = append(removeSensor(d.Temperatures, t.Sensor), temp)
	}
	if f := p.Firmware; f != nil {
		fw := model.Firmware{}
		if d.Firmware != nil {
			fw = *d.Firmware
		}
		setText(&fw.Current, f.Current)
		setText(&fw.Latest, f.Latest)
		if s, ok := r.text(f.UpdateAvailable); ok {
			fw.UpdateAvailable = model.Ptr(s == "true")
		}
		if fw != (model.Firmware{}) {
			d.Firmware = &fw
		}
	}
}

func removeSensor(list []model.Temperature, sensor string) []model.Temperature {
	out := list[:0]
	for _, t := range list {
		if t.Sensor != sensor {
			out = append(out, t)
		}
	}
	return out
}

// reader reads values, each OID once per device.
type reader struct {
	c     *client
	cache map[string][]gosnmp.SnmpPDU
}

func (r *reader) pdus(v *Value) []gosnmp.SnmpPDU {
	if r.cache == nil {
		r.cache = map[string][]gosnmp.SnmpPDU{}
	}
	key := v.Walk + "|" + v.OID
	if got, ok := r.cache[key]; ok {
		return got
	}
	var out []gosnmp.SnmpPDU
	if v.Walk == "" {
		if got, err := r.c.get(v.OID); err == nil {
			for _, p := range got {
				out = append(out, p)
			}
		}
	} else if got, err := r.c.walk(v.OID); err == nil {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return lessOID(keys[i], keys[j]) })
		for _, k := range keys {
			out = append(out, got[k])
		}
	}
	r.cache[key] = out
	return out
}

func lessOID(a, b string) bool {
	x, y := splitOID(a), splitOID(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}

var firstNumber = regexp.MustCompile(`-?\d+(\.\d+)?`)

// raw is the value of one PDU as a number (texts like "45 C" or "12 %" too).
func raw(p gosnmp.SnmpPDU) (float64, bool) {
	if v, ok := pduInt(p); ok {
		return float64(v), true
	}
	if v, ok := pduUint(p); ok {
		return float64(v), true
	}
	if m := firstNumber.FindString(pduString(p)); m != "" {
		f, err := strconv.ParseFloat(m, 64)
		return f, err == nil
	}
	return 0, false
}

func (r *reader) number(v *Value) (float64, bool) {
	if v == nil {
		return 0, false
	}
	var (
		f  float64
		ok bool
	)
	switch {
	case v.Total == nil && v.Used != nil && len(v.Free) > 0:
		used, okU := r.number(v.Used)
		free, okF := r.number(&v.Free[0])
		if !okU || !okF || used+free <= 0 {
			return 0, false
		}
		f, ok = used/(used+free)*100, true
	case v.Total != nil && (v.Used != nil || len(v.Free) > 0):
		total, okT := r.number(v.Total)
		if !okT || total <= 0 {
			return 0, false
		}
		if v.Used != nil {
			used, okU := r.number(v.Used)
			if !okU {
				return 0, false
			}
			f, ok = used/total*100, true
		} else {
			free := 0.0
			for i := range v.Free {
				x, okF := r.number(&v.Free[i])
				if !okF {
					continue // buffers or cached may be missing
				}
				free += x
			}
			f, ok = (1-free/total)*100, true
		}
	case v.OID != "":
		f, ok = r.combine(v)
	}
	if !ok {
		return 0, false
	}
	if v.Scale != 0 {
		f *= v.Scale
	}
	if v.Invert {
		f = 100 - f
	}
	return f, true
}

func (r *reader) combine(v *Value) (float64, bool) {
	var nums []float64
	for _, p := range r.pdus(v) {
		if f, ok := raw(p); ok {
			nums = append(nums, f)
		}
	}
	if len(nums) == 0 {
		return 0, false
	}
	switch v.Walk {
	case "", "first":
		return nums[0], true
	case "max":
		m := nums[0]
		for _, x := range nums {
			m = math.Max(m, x)
		}
		return m, true
	case "min":
		m := nums[0]
		for _, x := range nums {
			m = math.Min(m, x)
		}
		return m, true
	case "sum", "avg":
		s := 0.0
		for _, x := range nums {
			s += x
		}
		if v.Walk == "avg" {
			s /= float64(len(nums))
		}
		return s, true
	}
	slog.Debug("unknown walk in an SNMP profile", "walk", v.Walk)
	return 0, false
}

func (r *reader) text(v *Value) (string, bool) {
	if v == nil || v.OID == "" {
		return "", false
	}
	list := r.pdus(v)
	if len(list) == 0 {
		return "", false
	}
	p := list[0]
	s := strings.TrimSpace(pduString(p))
	if s == "" {
		if f, ok := raw(p); ok {
			s = strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	if v.Map != nil {
		mapped, ok := v.Map[s]
		if !ok {
			return "", false
		}
		s = mapped
	}
	return s, s != ""
}
