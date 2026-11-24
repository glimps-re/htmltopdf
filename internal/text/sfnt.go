package text

import (
	"encoding/binary"
	"errors"
)

// maxSfntTables bounds the table directory of a font; real fonts have a few
// dozen tables. It is the same cap as maxFontTables in internal/pdf, whose own
// parser reads the directory too: keep them equal.
const maxSfntTables = 128

const (
	sfntTrueType = 0x00010000
	sfntApple    = 0x74727565 // "true"
)

// checkSfnt validates the header and table directory of a TrueType font before
// the font loader trusts them: the loader's table reads (RawTable) allocate the
// length each table claims (and, for a WOFF, the size it claims to inflate to),
// up to 4 GiB. A table must lie inside the data, the tables together must not
// claim more than the data holds (overlapping records would each allocate the
// whole font), and anything but a plain sfnt is refused.
func checkSfnt(d []byte) error {
	const header, record = 12, 16
	if len(d) < header {
		return errors.New("truncated font header")
	}
	if m := binary.BigEndian.Uint32(d); m != sfntTrueType && m != sfntApple {
		return errors.New("not a TrueType font (only plain sfnt data is accepted)")
	}
	n := int(binary.BigEndian.Uint16(d[4:]))
	if n == 0 || n > maxSfntTables {
		return errors.New("invalid table count")
	}
	if len(d) < header+n*record {
		return errors.New("truncated table directory")
	}
	var claimed uint64
	for i := range n {
		rec := d[header+i*record:]
		off, length := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		if uint64(off)+uint64(length) > uint64(len(d)) {
			return errors.New("table outside the font data")
		}
		// The tables of a real font do not overlap: their lengths add up to less
		// than the data.
		if claimed += uint64(length); claimed > uint64(len(d)) {
			return errors.New("tables claim more than the font holds")
		}
	}
	return checkLayout(d)
}

// maxLayoutArrayBytes bounds what the offset lists of a GSUB or GPOS table
// declare in total (feature indexes, lookup indexes, subtable offsets): the
// parser allocates every declared array, even when many records point at the
// same bytes, so a small font could make it allocate gigabytes.
const maxLayoutArrayBytes = 1 << 20

// maxLayoutRecords bounds the records walked to add those arrays up.
const maxLayoutRecords = 1 << 16

// checkLayout refuses a font whose GSUB or GPOS declares more array data than
// maxLayoutArrayBytes, adding up the language systems of the script list, the
// features of the feature list and the subtable lists of the lookup list.
func checkLayout(d []byte) error {
	n := int(binary.BigEndian.Uint16(d[4:]))
	for i := range n {
		rec := d[12+i*16:]
		if tg := string(rec[:4]); tg != "GSUB" && tg != "GPOS" {
			continue
		}
		off, length := int(binary.BigEndian.Uint32(rec[8:])), int(binary.BigEndian.Uint32(rec[12:]))
		if err := checkLayoutTable(d[off : off+length]); err != nil {
			return err
		}
	}
	return nil
}

func be16(b []byte, off int) (int, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, false
	}
	return int(binary.BigEndian.Uint16(b[off:])), true
}

var errLayout = errors.New("GSUB or GPOS declares too much data")

// spendLangSys charges the feature indexes of the language system at off.
func spendLangSys(t []byte, off int, spend func(count, size int) bool) bool {
	c, _ := be16(t, off+4)
	return spend(c, 2)
}

func checkLayoutTable(t []byte) error {
	if len(t) < 10 {
		return nil // too short to hold the lists: the parser rejects it
	}
	budget := maxLayoutArrayBytes
	records := maxLayoutRecords
	spend := func(count, size int) bool {
		budget -= count * size
		records--
		return budget >= 0 && records >= 0
	}
	// Script list: scripts, then the language systems of each.
	if sl, _ := be16(t, 4); sl != 0 {
		count, ok := be16(t, sl)
		if !ok {
			return nil
		}
		for i := range count {
			so, ok := be16(t, sl+2+i*6+4)
			if !ok {
				return nil
			}
			script := sl + so
			def, _ := be16(t, script)
			langs, _ := be16(t, script+2)
			if def != 0 && !spendLangSys(t, script+def, spend) {
				return errLayout
			}
			for j := range langs {
				lo, ok := be16(t, script+4+j*6+4)
				if !ok {
					break
				}
				if !spendLangSys(t, script+lo, spend) {
					return errLayout
				}
			}
		}
	}
	// Feature list: the lookup indexes of each feature.
	if fl, _ := be16(t, 6); fl != 0 {
		count, _ := be16(t, fl)
		for i := range count {
			fo, ok := be16(t, fl+2+i*6+4)
			if !ok {
				break
			}
			c, _ := be16(t, fl+fo+2)
			if !spend(c, 2) {
				return errLayout
			}
		}
	}
	// Lookup list: the subtable offsets of each lookup.
	if ll, _ := be16(t, 8); ll != 0 {
		count, _ := be16(t, ll)
		for i := range count {
			lo, ok := be16(t, ll+2+i*2)
			if !ok {
				break
			}
			c, _ := be16(t, ll+lo+4)
			if !spend(c, 2) {
				return errLayout
			}
		}
	}
	return nil
}
