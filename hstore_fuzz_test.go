package pgxtypefaster_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/evanj/hacks/postgrestest"
	"github.com/evanj/pgxtypefaster"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// variants returns 6 variants from k1, v1, k2, v2:
//   - k1: NULL
//   - k1: v1
//   - k1: v1, k2: v2
//   - k1: NULL, k2: v2
//   - k1: v1, k2: NULL
//   - k1: NULL, k2: NULL
func variants(k1 string, v1 string, k2 string, v2 string) []pgxtypefaster.Hstore {
	return []pgxtypefaster.Hstore{
		{k1: pgxtypefaster.NewText(v1)},
		{k1: pgtype.Text{}},
		{k1: pgxtypefaster.NewText(v1), k2: pgxtypefaster.NewText(v2)},
		{k1: pgtype.Text{}, k2: pgxtypefaster.NewText(v2)},
		{k1: pgxtypefaster.NewText(v1), k2: pgtype.Text{}},
		{k1: pgtype.Text{}, k2: pgtype.Text{}},
	}
}

// validForHstore returns true if k1, v2, k2, v2 are valid values for an Hstore test:
//   - valid UTF-8
//   - does not contain the zero character: "\x00"
//   - k1 != k2
func validForHstore(k1 string, v1 string, k2 string, v2 string) bool {
	if k1 == k2 {
		return false
	}
	for _, str := range []string{k1, v1, k2, v2} {
		if !utf8.ValidString(str) {
			return false
		}
		if strings.ContainsRune(str, '\x00') {
			return false
		}
	}
	return true
}

func hstoreToString(h pgxtypefaster.Hstore) string {
	codec := pgxtypefaster.HstoreCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, h)
	out, err := codec.Encode(h, nil)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func hstoreToArray(h pgxtypefaster.Hstore) pgtype.FlatArray[pgtype.Text] {
	var out []pgtype.Text
	for k, v := range h {
		out = append(out, pgxtypefaster.NewText(k), v)
	}
	return out
}

// NOTE: This does not pass with the upstream version of pgx right now, since it
// does not round-trip
func FuzzLocalRoundTrip(f *testing.F) {
	f.Skip("TODO")
	f.Add("", "", "", "")
	f.Add("k1", "v1", "k2", "v2")
	f.Add(`\`, `"`, `,`, "v2")

	f.Fuzz(func(t *testing.T, k1 string, v1 string, k2 string, v2 string) {
		if !validForHstore(k1, v1, k2, v2) {
			return
		}

		for _, variant := range variants(k1, v1, k2, v2) {
			for _, cfg := range allHstoreConfigs {
				input := cfg.fasterHstoreToConfigType(variant)

				serialized, err := cfg.encodePlan.Encode(input, nil)
				if err != nil {
					t.Fatalf("cfg=%s input=%s: failed to encode: %s",
						cfg.name, hstoreToString(variant), err)
				}

				output := cfg.newScanType()
				err = cfg.scanPlan.Scan(serialized, output)
				if err != nil {
					t.Fatalf("cfg=%s input=%s: failed to scan: %s",
						cfg.name, hstoreToString(variant), err)
				}
				// output is a pointer to an hstore type: dereference that pointer
				outputDeref := reflect.ValueOf(output).Elem().Interface()
				if !reflect.DeepEqual(outputDeref, input) {
					t.Fatalf("cfg=%s input=%s: output != input\n  output=%#v\n  input=%#v",
						cfg.name, hstoreToString(variant), output, input)

				}

				// TODO: database/sql always uses the text encoding, so this is duplicated
				valuer := outputDeref.(driver.Valuer)
				sqlValue, err := valuer.Value()
				if err != nil {
					t.Fatalf("cfg=%s input=%s: failed to call database/sql.Value: %s",
						cfg.name, hstoreToString(variant), err)
				}
				sqlOutput := cfg.newScanType()
				err = sqlOutput.(sql.Scanner).Scan(sqlValue)
				if err != nil {
					t.Fatalf("cfg=%s input=%s: failed to call database/sql.Scan: %s",
						cfg.name, hstoreToString(variant), err)
				}
				sqlOutputDeref := reflect.ValueOf(output).Elem().Interface()
				if !reflect.DeepEqual(sqlOutputDeref, outputDeref) {
					t.Fatalf("cfg=%s input=%s: database/sql output != input\n  output=%#v\n  input=%#v",
						cfg.name, hstoreToString(variant), sqlOutputDeref, outputDeref)

				}
			}
		}
	})
}

// FuzzPGRoundTrip uses Postgres itself to fuzz the Hstore type.
func FuzzPGRoundTrip(f *testing.F) {
	pgURL := postgrestest.New(f)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "create extension hstore")
	if err != nil {
		panic(err)
	}
	err = pgxtypefaster.RegisterHstore(ctx, conn)
	if err != nil {
		panic(err)
	}

	f.Add("", "", "", "")
	f.Add("k1", "v1", "k2", "v2")
	// escaped characters
	f.Add(`\`, `"`, `,`, "v2")

	// Postgres limitation: "the character with code zero (sometimes called NUL) cannot be stored"
	// https://www.postgresql.org/docs/current/datatype-character.html
	f.Add("k1", "v1", "k2", "zero byte: \x00")

	// Invalid UTF-8 must be filtered out of the test
	f.Add("k1", "v1", "k2", "invalid UTF-8: \xaf foo")

	// pgx array encoding previous had a bug with vtab
	f.Add("k1", "v1", "k2", "VTAB:\v")

	// this query converts an array to hstore and compares it to the input
	// this tests serializing an hstore and deserializing it
	const query = `select hstore_from_array, hstore_from_array = hstore_input AS hstores_equal from (
	select hstore($1::text[]) as hstore_from_array, $2::hstore as hstore_input
) as hstore_input_query`

	f.Fuzz(func(t *testing.T, k1 string, v1 string, k2 string, v2 string) {
		if !validForHstore(k1, v1, k2, v2) {
			return
		}

		for _, variant := range variants(k1, v1, k2, v2) {
			outputHstore := pgxtypefaster.Hstore{}
			postgresEqual := false

			// these modes use the text and binary protocols, respectively
			for _, queryMode := range []pgx.QueryExecMode{pgx.QueryExecModeSimpleProtocol, pgx.QueryExecModeDescribeExec} {
				row := conn.QueryRow(ctx, query, queryMode, hstoreToArray(variant), variant)
				err = row.Scan(&outputHstore, &postgresEqual)
				if err != nil {
					t.Fatalf("variant=%s queryMode=%s: Scan failed: %s",
						hstoreToString(variant), queryMode.String(), err)
				}

				if !reflect.DeepEqual(outputHstore, variant) {
					t.Errorf("variant=%#v queryMode=%s: postgres parsed value does not match input; output=%s",
						hstoreToString(variant), queryMode.String(), hstoreToString(outputHstore))
				}
				if !postgresEqual {
					t.Errorf("variant=%s queryMode=%s: postgres did not think the values were equal",
						hstoreToString(variant), queryMode.String())
				}
			}
		}
	})
}
