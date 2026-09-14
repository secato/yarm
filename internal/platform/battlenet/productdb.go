// product.db is a serialized ProductDb protobuf message. The schema is
// published — Lutris and dlss-swapper both carry a copy of it, derived
// from the Battle.net agent — and only two of its fields matter for
// discovery: each ProductInstall's uid and its settings' install path.
// cached_product_state.base_product_state.installed is deliberately not
// one of them: on a wine-run agent it reads false for games that are
// verifiably, fully installed (confirmed against a real Diablo IV
// install driven by Omarchy's Battle.net installer, cross-checked byte
// for byte against Lutris' own decoder), so it is not a signal this
// package trusts — Lutris' own scanner does not gate on it either,
// leaning on the same thing this package leans on instead: whether the
// install path actually exists on disk (sources.Entry.Game's job).
// Instead of a generated codec, this walks the wire format directly:
// tag varints, varint and length-delimited values, descending only into
// field numbers the schema defines. Recursion depth is bounded by the
// schema itself (four levels), and the file is read into memory under
// a size cap — product.db lives in a directory yarm does not own, the
// same reasoning that bounds the Steam VDF parser.
package battlenet

import (
	"errors"
	"fmt"
	"os"

	"github.com/secato/yarm/internal/fsutil"
)

// maxProductDBBytes bounds the file. The real thing is tens of
// kilobytes; this leaves room without leaving room for abuse.
const maxProductDBBytes = 4 << 20

// productInstall is what discovery needs from one ProductInstall
// message: which product it is and where it is installed.
type productInstall struct {
	uid         string
	installPath string
}

// readProductDB reads and parses one product.db. A missing file is not
// an error — callers probe locations where Battle.net may never have
// been installed — but an oversized or unparsable one is.
func readProductDB(path string) ([]productInstall, error) {
	data, err := fsutil.ReadFileMax(path, maxProductDBBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("battlenet: %w", err)
	}

	products, err := parseProductDB(data)
	if err != nil {
		return nil, fmt.Errorf("battlenet: %s: %w", path, err)
	}
	return products, nil
}

// The protobuf wire types this parser handles. Groups (3 and 4) were
// dropped from the language and are not in this schema; seeing one
// means the file is not the product.db the schema describes.
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

// wireField is one decoded field of a message.
type wireField struct {
	num    int
	wire   int
	varint uint64
	data   []byte // set for wireBytes
}

// parseProductDB reads a ProductDb: field 1 is the repeated
// ProductInstall list. Fields the schema defines elsewhere are
// skipped; a body that cannot be walked is an error, because the only
// honest thing to do with a product.db that is not one is say so.
func parseProductDB(data []byte) ([]productInstall, error) {
	fields, err := parseWireFields(data)
	if err != nil {
		return nil, err
	}

	var products []productInstall
	for _, f := range fields {
		if f.num != 1 || f.wire != wireBytes {
			continue
		}
		pi, err := parseProductInstall(f.data)
		if err != nil {
			return nil, err
		}
		products = append(products, pi)
	}
	return products, nil
}

// parseProductInstall reads one ProductInstall: uid (1), product_code
// (2), settings (3). cached_product_state (4) is deliberately not read
// — see the package doc comment.
func parseProductInstall(b []byte) (productInstall, error) {
	fields, err := parseWireFields(b)
	if err != nil {
		return productInstall{}, err
	}

	var pi productInstall
	for _, f := range fields {
		switch f.num {
		case 1:
			if f.wire == wireBytes {
				pi.uid = string(f.data)
			}
		case 3:
			if f.wire == wireBytes {
				pi.installPath = parseInstallPath(f.data)
			}
		}
	}
	return pi, nil
}

// parseInstallPath reads a UserSettings message: install_path (1).
func parseInstallPath(b []byte) string {
	fields, err := parseWireFields(b)
	if err != nil {
		return ""
	}
	for _, f := range fields {
		if f.num == 1 && f.wire == wireBytes {
			return string(f.data)
		}
	}
	return ""
}

var errTruncated = errors.New("truncated message")

// parseWireFields splits one message body into its fields, skipping
// the values of fields this parser does not ask about by number.
func parseWireFields(b []byte) ([]wireField, error) {
	var fields []wireField
	for len(b) > 0 {
		tag, n, err := readVarint(b)
		if err != nil {
			return nil, err
		}
		b = b[n:]

		num, wt := int(tag>>3), int(tag&7)
		if num == 0 {
			return nil, errors.New("field number 0")
		}

		var f wireField
		f.num, f.wire = num, wt
		switch wt {
		case wireVarint:
			v, n, err := readVarint(b)
			if err != nil {
				return nil, err
			}
			b = b[n:]
			f.varint = v
		case wireFixed64:
			if len(b) < 8 {
				return nil, errTruncated
			}
			b = b[8:]
		case wireBytes:
			l, n, err := readVarint(b)
			if err != nil {
				return nil, err
			}
			b = b[n:]
			if l > uint64(len(b)) {
				return nil, errTruncated
			}
			f.data = b[:l]
			b = b[l:]
		case wireFixed32:
			if len(b) < 4 {
				return nil, errTruncated
			}
			b = b[4:]
		default:
			return nil, fmt.Errorf("unsupported wire type %d", wt)
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// readVarint reads one base-128 varint, returning its value and how
// many bytes it consumed. Ten bytes is the most a 64-bit varint can
// take; an unterminated one is truncated, not zero-padded.
func readVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * uint(i))
		if b[i]&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errTruncated
}
