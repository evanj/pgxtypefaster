// Package pgxtypefaster provides types for use with the pgx Postgres driver that are faster,
// but not completely API compatible.
package pgxtypefaster

import (
	"context"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/evanj/pgxtypefaster/internal/pgio"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type HstoreScanner interface {
	ScanHstore(v Hstore) error
}

type HstoreValuer interface {
	HstoreValue() (Hstore, error)
}

var ErrHstoreDoesNotExist = errors.New("postgres type hstore does not exist (the extension may not be loaded)")

// queryHstoreOID returns the Postgres Object Identifer (OID) for the "hstore" type. This must be
// done for each separate Postgres database, since the OID can be different. It returns
// errHstoreDoesNotExist if the row does not exist.
func queryHstoreOID(ctx context.Context, conn *pgx.Conn) (uint32, error) {
	// get the hstore OID: it varies because hstore is an extension and not built-in
	var hstoreOID uint32
	err := conn.QueryRow(ctx, `select oid from pg_type where typname = 'hstore'`).Scan(&hstoreOID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrHstoreDoesNotExist
		}
		return 0, err
	}
	return hstoreOID, nil
}

// RegisterHstore registers the Hstore type with conn's default type map. It queries the database
// for the Hstore OID to be able to register it.
func RegisterHstore(ctx context.Context, conn *pgx.Conn) error {
	hstoreOID, err := queryHstoreOID(ctx, conn)
	if err != nil {
		return err
	}
	conn.TypeMap().RegisterType(&pgtype.Type{Codec: HstoreCodec{}, Name: "hstore", OID: hstoreOID})
	return nil
}

// Hstore represents an hstore column that can be null or have null values
// associated with its keys.
type Hstore map[string]pgtype.Text

func (h *Hstore) ScanHstore(v Hstore) error {
	*h = v
	return nil
}

func (h Hstore) HstoreValue() (Hstore, error) {
	return h, nil
}

// NewText converts a string to a non-NULL pgtype.Text.
func NewText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

// PGXToFasterHstore copies a pgtype.Hstore into a pgxtypefaster.Hstore.
func PGXToFasterHstore(m map[string]*string) Hstore {
	h := make(Hstore, len(m))
	for k, v := range m {
		if v == nil {
			h[k] = pgtype.Text{Valid: false}
		} else {
			h[k] = NewText(*v)
		}
	}
	return h
}

// Scan implements the database/sql Scanner interface.
func (h *Hstore) Scan(src any) error {
	if src == nil {
		*h = nil
		return nil
	}

	switch src := src.(type) {
	case string:
		return scanPlanTextAnyToHstoreScanner{}.scanString(src, h)
	}

	return fmt.Errorf("cannot scan %T", src)
}

// Value implements the database/sql/driver Valuer interface.
func (h Hstore) Value() (driver.Value, error) {
	if h == nil {
		return nil, nil
	}

	buf, err := HstoreCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, h).Encode(h, nil)
	if err != nil {
		return nil, err
	}
	return string(buf), err
}

type HstoreCodec struct{}

func (HstoreCodec) FormatSupported(format int16) bool {
	return format == pgtype.TextFormatCode || format == pgtype.BinaryFormatCode
}

func (HstoreCodec) PreferredFormat() int16 {
	return pgtype.BinaryFormatCode
}

func (HstoreCodec) PlanEncode(m *pgtype.Map, oid uint32, format int16, value any) pgtype.EncodePlan {
	if _, ok := value.(HstoreValuer); !ok {
		return nil
	}

	switch format {
	case pgtype.BinaryFormatCode:
		return encodePlanHstoreCodecBinary{}
	case pgtype.TextFormatCode:
		return encodePlanHstoreCodecText{}
	}

	return nil
}

type encodePlanHstoreCodecBinary struct{}

func (encodePlanHstoreCodecBinary) Encode(value any, buf []byte) (newBuf []byte, err error) {
	hstore, err := value.(HstoreValuer).HstoreValue()
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

		if v.Valid {
			buf = pgio.AppendInt32(buf, int32(len(v.String)))
			buf = append(buf, (v.String)...)
		} else {
			buf = pgio.AppendInt32(buf, -1)
		}
	}

	return buf, nil
}

type encodePlanHstoreCodecText struct{}

func (encodePlanHstoreCodecText) Encode(value any, buf []byte) (newBuf []byte, err error) {
	hstore, err := value.(HstoreValuer).HstoreValue()
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

		if v.Valid {
			buf = append(buf, '"')
			buf = append(buf, quoteArrayReplacer.Replace(v.String)...)
			buf = append(buf, '"')
		} else {
			buf = append(buf, "NULL"...)
		}
	}

	return buf, nil
}

func (HstoreCodec) PlanScan(m *pgtype.Map, oid uint32, format int16, target any) pgtype.ScanPlan {

	switch format {
	case pgtype.BinaryFormatCode:
		switch target.(type) {
		case HstoreScanner:
			return scanPlanBinaryHstoreToHstoreScanner{}
		}
	case pgtype.TextFormatCode:
		switch target.(type) {
		case HstoreScanner:
			return scanPlanTextAnyToHstoreScanner{}
		}
	}

	return nil
}

type scanPlanBinaryHstoreToHstoreScanner struct{}

func (scanPlanBinaryHstoreToHstoreScanner) Scan(src []byte, dst any) error {
	scanner := (dst).(HstoreScanner)

	if src == nil {
		return scanner.ScanHstore(Hstore(nil))
	}

	rp := 0

	const uint32Len = 4
	if len(src[rp:]) < uint32Len {
		return fmt.Errorf("hstore incomplete %v", src)
	}
	pairCount := int(int32(binary.BigEndian.Uint32(src[rp:])))
	rp += uint32Len

	hstore := make(Hstore, pairCount)
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
			value := string(keyValueString[rp-uint32Len : rp-uint32Len+valueLen])
			rp += valueLen

			hstore[key] = pgtype.Text{String: value, Valid: true}
		} else {
			hstore[key] = pgtype.Text{String: "", Valid: false}
		}
	}

	return scanner.ScanHstore(hstore)
}

type scanPlanTextAnyToHstoreScanner struct{}

func (s scanPlanTextAnyToHstoreScanner) Scan(src []byte, dst any) error {
	scanner := (dst).(HstoreScanner)

	if src == nil {
		return scanner.ScanHstore(Hstore(nil))
	}
	return s.scanString(string(src), scanner)
}

// scanString does not return nil hstore values because string cannot be nil.
func (scanPlanTextAnyToHstoreScanner) scanString(src string, scanner HstoreScanner) error {
	hstore, err := parseHstore(src)
	if err != nil {
		return err
	}
	return scanner.ScanHstore(hstore)
}

func (c HstoreCodec) DecodeDatabaseSQLValue(m *pgtype.Map, oid uint32, format int16, src []byte) (driver.Value, error) {
	return codecDecodeToTextFormat(c, m, oid, format, src)
}

func (c HstoreCodec) DecodeValue(m *pgtype.Map, oid uint32, format int16, src []byte) (any, error) {
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

type hstoreParser struct {
	str           string
	pos           int
	nextBackslash int
}

func newHSP(in string) *hstoreParser {
	return &hstoreParser{
		pos:           0,
		str:           in,
		nextBackslash: strings.IndexByte(in, '\\'),
	}
}

func (p *hstoreParser) atEnd() bool {
	return p.pos >= len(p.str)
}

// consume returns the next byte of the string, or end if the string is done.
func (p *hstoreParser) consume() (b byte, end bool) {
	if p.pos >= len(p.str) {
		return 0, true
	}
	b = p.str[p.pos]
	p.pos++
	return b, false
}

func unexpectedByteErr(actualB byte, expectedB byte) error {
	return fmt.Errorf("expected '%c' ('%#v'); found '%c' ('%#v')", expectedB, expectedB, actualB, actualB)
}

// consumeExpectedByte consumes expectedB from the string, or returns an error.
func (p *hstoreParser) consumeExpectedByte(expectedB byte) error {
	nextB, end := p.consume()
	if end {
		return fmt.Errorf("expected '%c' ('%#v'); found end", expectedB, expectedB)
	}
	if nextB != expectedB {
		return unexpectedByteErr(nextB, expectedB)
	}
	return nil
}

// consumeExpected2 consumes two expected bytes or returns an error.
// This was a bit faster than using a string argument (better inlining? Not sure).
func (p *hstoreParser) consumeExpected2(one byte, two byte) error {
	if p.pos+2 > len(p.str) {
		return errors.New("unexpected end of string")
	}
	if p.str[p.pos] != one {
		return unexpectedByteErr(p.str[p.pos], one)
	}
	if p.str[p.pos+1] != two {
		return unexpectedByteErr(p.str[p.pos+1], two)
	}
	p.pos += 2
	return nil
}

var errEOSInQuoted = errors.New(`found end before closing double-quote ('"')`)

// consumeDoubleQuoted consumes a double-quoted string from p. The double quote must have been
// parsed already.
func (p *hstoreParser) consumeDoubleQuoted() (string, error) {
	// fast path: assume most keys/values do not contain escapes
	nextDoubleQuote := strings.IndexByte(p.str[p.pos:], '"')
	if nextDoubleQuote == -1 {
		return "", errEOSInQuoted
	}
	nextDoubleQuote += p.pos
	if p.nextBackslash == -1 || p.nextBackslash > nextDoubleQuote {
		// no escapes in this string
		s := p.str[p.pos:nextDoubleQuote]
		p.pos = nextDoubleQuote + 1
		return s, nil
	}

	// slow path: string contains escapes
	s, err := p.consumeDoubleQuotedWithEscapes(p.nextBackslash)
	p.nextBackslash = strings.IndexByte(p.str[p.pos:], '\\')
	if p.nextBackslash != -1 {
		p.nextBackslash += p.pos
	}
	return s, err
}

// consumeDoubleQuotedWithEscapes consumes a double-quoted string containing escapes, starting
// at p.pos, and with the first backslash at firstBackslash.
func (p *hstoreParser) consumeDoubleQuotedWithEscapes(firstBackslash int) (string, error) {
	// copy the prefix that does not contain backslashes
	var builder strings.Builder
	builder.WriteString(p.str[p.pos:firstBackslash])

	// skip to the backslash
	p.pos = firstBackslash

	// copy bytes until the end, unescaping backslashes
	for {
		nextB, end := p.consume()
		if end {
			return "", errEOSInQuoted
		} else if nextB == '"' {
			break
		} else if nextB == '\\' {
			// escape: skip the backslash and copy the char
			nextB, end = p.consume()
			if end {
				return "", errEOSInQuoted
			}
			if !(nextB == '\\' || nextB == '"') {
				return "", fmt.Errorf("unexpected escape in quoted string: found '%#v'", nextB)
			}
			builder.WriteByte(nextB)
		} else {
			// normal byte: copy it
			builder.WriteByte(nextB)
		}
	}
	return builder.String(), nil
}

// consumePairSeparator consumes the Hstore pair separator ", " or returns an error.
func (p *hstoreParser) consumePairSeparator() error {
	return p.consumeExpected2(',', ' ')
}

// consumeKVSeparator consumes the Hstore key/value separator "=>" or returns an error.
func (p *hstoreParser) consumeKVSeparator() error {
	return p.consumeExpected2('=', '>')
}

// consumeDoubleQuotedOrNull consumes the Hstore key/value separator "=>" or returns an error.
func (p *hstoreParser) consumeDoubleQuotedOrNull() (pgtype.Text, error) {
	// peek at the next byte
	if p.atEnd() {
		return pgtype.Text{}, errors.New("found end instead of value")
	}
	next := p.str[p.pos]
	if next == 'N' {
		// must be the exact string NULL: use consumeExpected2 twice
		err := p.consumeExpected2('N', 'U')
		if err != nil {
			return pgtype.Text{}, err
		}
		err = p.consumeExpected2('L', 'L')
		if err != nil {
			return pgtype.Text{}, err
		}
		return pgtype.Text{String: "", Valid: false}, nil
	} else if next != '"' {
		return pgtype.Text{}, unexpectedByteErr(next, '"')
	}

	// skip the double quote
	p.pos += 1
	s, err := p.consumeDoubleQuoted()
	if err != nil {
		return pgtype.Text{}, err
	}
	return NewText(s), nil
}

func parseHstore(s string) (Hstore, error) {
	p := newHSP(s)

	// This is an over-estimate of the number of key/value pairs. Use '>' because I am guessing it
	// is less likely to occur in keys/values than '=' or ','.
	numPairsEstimate := strings.Count(s, ">")
	result := make(Hstore, numPairsEstimate)
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
		result[key] = value
	}

	return result, nil
}
