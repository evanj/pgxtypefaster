// Package pgxtypefaster provides types for use with the pgx Postgres driver that are faster,
// but not completely API compatible.
package pgxtypefaster

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/evanj/pgxtypefaster/internal/pgio"
	"github.com/jackc/pgx/v5/pgtype"
)

type HstoreCompatScanner interface {
	ScanHstoreCompat(v HstoreCompat) error
}

type HstoreCompatValuer interface {
	HstoreCompatValue() (HstoreCompat, error)
}

// HstoreCompat represents an hstore column that can be null or have null values
// associated with its keys.
type HstoreCompat map[string]*string

func (h *HstoreCompat) ScanHstoreCompat(v HstoreCompat) error {
	*h = v
	return nil
}

func (h HstoreCompat) HstoreCompatValue() (HstoreCompat, error) {
	return h, nil
}

// Scan implements the database/sql Scanner interface.
func (h *HstoreCompat) Scan(src any) error {
	if src == nil {
		*h = nil
		return nil
	}

	switch src := src.(type) {
	case string:
		return scanPlanTextAnyToHstoreCompatScanner{}.scanString(src, h)
	}

	return fmt.Errorf("cannot scan %T", src)
}

// Value implements the database/sql/driver Valuer interface.
func (h HstoreCompat) Value() (driver.Value, error) {
	if h == nil {
		return nil, nil
	}

	buf, err := HstoreCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, h).Encode(h, nil)
	if err != nil {
		return nil, err
	}
	return string(buf), err
}

type HstoreCompatCodec struct{}

func (HstoreCompatCodec) FormatSupported(format int16) bool {
	return format == pgtype.TextFormatCode || format == pgtype.BinaryFormatCode
}

func (HstoreCompatCodec) PreferredFormat() int16 {
	return pgtype.BinaryFormatCode
}

func (HstoreCompatCodec) PlanEncode(m *pgtype.Map, oid uint32, format int16, value any) pgtype.EncodePlan {
	if _, ok := value.(HstoreCompatValuer); !ok {
		return nil
	}

	switch format {
	case pgtype.BinaryFormatCode:
		return encodePlanHstoreCompatCodecBinary{}
	case pgtype.TextFormatCode:
		return encodePlanHstoreCompatCodecText{}
	}

	return nil
}

type encodePlanHstoreCompatCodecBinary struct{}

func (encodePlanHstoreCompatCodecBinary) Encode(value any, buf []byte) (newBuf []byte, err error) {
	hstore, err := value.(HstoreCompatValuer).HstoreCompatValue()
	if err != nil {
		return nil, err
	}

	if hstore == nil {
		return nil, nil
	}

	buf = pgio.AppendInt32(buf, int32(len(hstore)))

	for k, v := range hstore {
		buf = pgio.AppendInt32(buf, int32(len(k)))
		buf = append(buf, k...)

		if v == nil {
			buf = pgio.AppendInt32(buf, -1)
		} else {
			buf = pgio.AppendInt32(buf, int32(len(*v)))
			buf = append(buf, (*v)...)
		}
	}

	return buf, nil
}

type encodePlanHstoreCompatCodecText struct{}

func (encodePlanHstoreCompatCodecText) Encode(value any, buf []byte) (newBuf []byte, err error) {
	hstore, err := value.(HstoreCompatValuer).HstoreCompatValue()
	if err != nil {
		return nil, err
	}

	if hstore == nil {
		return nil, nil
	}

	firstPair := true

	for k, v := range hstore {
		if firstPair {
			firstPair = false
		} else {
			buf = append(buf, ',', ' ')
		}

		// unconditionally quote hstore keys/values like Postgres does
		// this avoids a Mac OS X Postgres hstore parsing bug:
		// https://www.postgresql.org/message-id/CA%2BHWA9awUW0%2BRV_gO9r1ABZwGoZxPztcJxPy8vMFSTbTfi4jig%40mail.gmail.com
		buf = append(buf, '"')
		buf = append(buf, quoteArrayReplacer.Replace(k)...)
		buf = append(buf, '"')
		buf = append(buf, "=>"...)

		if v == nil {
			buf = append(buf, "NULL"...)
		} else {
			buf = append(buf, '"')
			buf = append(buf, quoteArrayReplacer.Replace(*v)...)
			buf = append(buf, '"')
		}
	}

	return buf, nil
}

func (HstoreCompatCodec) PlanScan(m *pgtype.Map, oid uint32, format int16, target any) pgtype.ScanPlan {

	switch format {
	case pgtype.BinaryFormatCode:
		switch target.(type) {
		case HstoreCompatScanner:
			return scanPlanBinaryHstoreToHstoreCompatScanner{}
		}
	case pgtype.TextFormatCode:
		switch target.(type) {
		case HstoreCompatScanner:
			return scanPlanTextAnyToHstoreCompatScanner{}
		}
	}

	return nil
}

type scanPlanBinaryHstoreToHstoreCompatScanner struct{}

func (scanPlanBinaryHstoreToHstoreCompatScanner) Scan(src []byte, dst any) error {
	scanner := (dst).(HstoreCompatScanner)

	if src == nil {
		return scanner.ScanHstoreCompat(HstoreCompat(nil))
	}

	rp := 0

	const uint32Len = 4
	if len(src[rp:]) < uint32Len {
		return fmt.Errorf("hstore incomplete %v", src)
	}
	pairCount := int(int32(binary.BigEndian.Uint32(src[rp:])))
	rp += uint32Len

	hstore := make(HstoreCompat, pairCount)
	// one allocation for all *string, rather than one per string, just like text parsing
	valueStrings := make([]string, pairCount)
	// one shared string for all key/value strings
	keyValueString := string(src[rp:])

	for i := 0; i < pairCount; i++ {
		if len(src[rp:]) < uint32Len {
			return fmt.Errorf("hstore incomplete %v", src)
		}
		keyLen := int(int32(binary.BigEndian.Uint32(src[rp:])))
		rp += uint32Len

		if len(src[rp:]) < keyLen {
			return fmt.Errorf("hstore incomplete %v", src)
		}
		key := string(keyValueString[rp-uint32Len : rp-uint32Len+keyLen])
		rp += keyLen

		if len(src[rp:]) < uint32Len {
			return fmt.Errorf("hstore incomplete %v", src)
		}
		valueLen := int(int32(binary.BigEndian.Uint32(src[rp:])))
		rp += 4

		if valueLen >= 0 {
			valueStrings[i] = string(keyValueString[rp-uint32Len : rp-uint32Len+valueLen])
			rp += valueLen

			hstore[key] = &valueStrings[i]
		} else {
			hstore[key] = nil
		}
	}

	return scanner.ScanHstoreCompat(hstore)
}

type scanPlanTextAnyToHstoreCompatScanner struct{}

func (s scanPlanTextAnyToHstoreCompatScanner) Scan(src []byte, dst any) error {
	scanner := (dst).(HstoreCompatScanner)

	if src == nil {
		return scanner.ScanHstoreCompat(HstoreCompat(nil))
	}
	return s.scanString(string(src), scanner)
}

// scanString does not return nil hstore values because string cannot be nil.
func (scanPlanTextAnyToHstoreCompatScanner) scanString(src string, scanner HstoreCompatScanner) error {
	hstore, err := parseHstoreCompat(src)
	if err != nil {
		return err
	}
	return scanner.ScanHstoreCompat(hstore)
}

func (c HstoreCompatCodec) DecodeDatabaseSQLValue(m *pgtype.Map, oid uint32, format int16, src []byte) (driver.Value, error) {
	return codecDecodeToTextFormat(c, m, oid, format, src)
}

func (c HstoreCompatCodec) DecodeValue(m *pgtype.Map, oid uint32, format int16, src []byte) (any, error) {
	if src == nil {
		return nil, nil
	}

	var hstore Hstore
	err := codecScan(c, m, oid, format, src, &hstore)
	if err != nil {
		return nil, err
	}
	return hstore, nil
}

func parseHstoreCompat(s string) (HstoreCompat, error) {
	p := newHSP(s)

	// This is an over-estimate of the number of key/value pairs. Use '>' because I am guessing it
	// is less likely to occur in keys/values than '=' or ','.
	numPairsEstimate := strings.Count(s, ">")
	result := make(HstoreCompat, numPairsEstimate)
	// makes one allocation of strings for the entire Hstore, rather than one allocation per value.
	valueStrings := make([]string, 0, numPairsEstimate)
	first := true
	for !p.atEnd() {
		if !first {
			err := p.consumePairSeparator()
			if err != nil {
				return nil, err
			}
		} else {
			first = false
		}

		err := p.consumeExpectedByte('"')
		if err != nil {
			return nil, err
		}

		key, err := p.consumeDoubleQuoted()
		if err != nil {
			return nil, err
		}

		err = p.consumeKVSeparator()
		if err != nil {
			return nil, err
		}

		value, err := p.consumeDoubleQuotedOrNull()
		if err != nil {
			return nil, err
		}
		if value.Valid {
			valueStrings = append(valueStrings, value.String)
			result[key] = &valueStrings[len(valueStrings)-1]
		} else {
			result[key] = nil
		}
	}

	return result, nil
}
