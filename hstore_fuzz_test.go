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

func isScannedHstoreEqual(input any, scanOutput any) bool {
	// scanOutput is a pointer to an hstore type: dereference that pointer
	outputDeref := reflect.ValueOf(scanOutput).Elem().Interface()
	return reflect.DeepEqual(outputDeref, input)
}

func FuzzLocalRoundTrip(f *testing.F) {
	f.Add("", "", "a", "")
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
				if !isScannedHstoreEqual(input, output) {
					t.Fatalf("cfg=%s input=%s: output != input\n  output=%#v\n  input=%#v",
						cfg.name, hstoreToString(variant), output, input)
				}

				// TODO: database/sql always uses the text encoding, so this is duplicated
				valuer := output.(driver.Valuer)
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

				if !isScannedHstoreEqual(input, sqlOutput) {
					t.Fatalf("cfg=%s input=%s: database/sql output != input\n  output=%#v\n  input=%#v",
						cfg.name, hstoreToString(variant), sqlOutput, input)

				}
			}
		}
	})
}

// copied from pgxtypefaster TODO: refactor to reuse these functions
func queryHstoreOID(ctx context.Context, conn *pgx.Conn) (uint32, error) {
	// get the hstore OID: it varies because hstore is an extension and not built-in
	var hstoreOID uint32
	err := conn.QueryRow(ctx, `select oid from pg_type where typname = 'hstore'`).Scan(&hstoreOID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, pgxtypefaster.ErrHstoreDoesNotExist
		}
		return 0, err
	}
	return hstoreOID, nil
}

// copied from pgxtypefaster TODO: refactor to reuse these functions
func registerPGXHstore(ctx context.Context, conn *pgx.Conn) error {
	hstoreOID, err := queryHstoreOID(ctx, conn)
	if err != nil {
		return err
	}
	conn.TypeMap().RegisterType(&pgtype.Type{Codec: pgtype.HstoreCodec{}, Name: "hstore", OID: hstoreOID})
	return nil
}

// FuzzPGRoundTrip uses Postgres itself to fuzz the Hstore type.
func FuzzPGRoundTrip(f *testing.F) {
	pgURL := postgrestest.New(f)
	ctx := context.Background()
	connFasterHstore, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		panic(err)
	}
	defer connFasterHstore.Close(ctx)

	_, err = connFasterHstore.Exec(ctx, "create extension hstore")
	if err != nil {
		panic(err)
	}
	err = pgxtypefaster.RegisterHstore(ctx, connFasterHstore)
	if err != nil {
		panic(err)
	}

	connPGXHstore, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		panic(err)
	}
	defer connPGXHstore.Close(ctx)
	err = registerPGXHstore(ctx, connPGXHstore)
	if err != nil {
		panic(err)
	}

	connHstoreCompat, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		panic(err)
	}
	defer connHstoreCompat.Close(ctx)
	err = pgxtypefaster.RegisterHstoreCompat(ctx, connHstoreCompat)
	if err != nil {
		panic(err)
	}

	f.Add("", "", "a", "")
	f.Add("k1", "v1", "k2", "v2")

	// escaped characters
	f.Add(`\`, `"`, `,`, "v2")

	// Postgres limitation: "the character with code zero (sometimes called NUL) cannot be stored"
	// https://www.postgresql.org/docs/current/datatype-character.html
	// this will not be tested at all
	f.Add("k1", "v1", "k2", "zero byte: \x00")

	// Invalid UTF-8 must be filtered out of the test
	f.Add("k1", "v1", "k2", "invalid UTF-8: \xaf foo")

	// pgx array encoding previous had a bug with vtab
	f.Add("k1", "v1", "k2", "VTAB:\v")

	// Postgres + pgx hstore encoding had a bug with these characters
	f.Add("k1", "mac_bugą", "mac_bugą", "mac_bugą")

	// previous pgx bug with whitespace in hstore
	f.Add("\n", "\t", "\v", "\f")

	// this query converts an array to hstore and compares it to the input
	// this tests serializing an hstore and deserializing it
	const query = `select hstore_from_array, hstore_from_array = hstore_input AS hstores_equal from (
	select hstore($1::text[]) as hstore_from_array, $2::hstore as hstore_input
) as hstore_input_query`

	connConfigs := []struct {
		conn        *pgx.Conn
		codecConfig hstoreTestCodecConfig
	}{
		// first record is pgxfastertype.Hstore
		{connFasterHstore, allHstoreConfigs[0]},
		// second record is pgxfastertype.HstoreCompat
		{connHstoreCompat, allHstoreConfigs[1]},
		// second record is pgtype
		{connPGXHstore, allHstoreConfigs[2]},
	}

	f.Fuzz(func(t *testing.T, k1 string, v1 string, k2 string, v2 string) {
		if !validForHstore(k1, v1, k2, v2) {
			return
		}

		for _, variant := range variants(k1, v1, k2, v2) {
			postgresEqual := false

			// these modes use the text and binary protocols, respectively
			for _, queryMode := range []pgx.QueryExecMode{pgx.QueryExecModeSimpleProtocol, pgx.QueryExecModeDescribeExec} {
				for _, connConfig := range connConfigs {
					input := connConfig.codecConfig.fasterHstoreToConfigType(variant)
					row := connConfig.conn.QueryRow(ctx, query, queryMode, hstoreToArray(variant), input)
					outputHstore := connConfig.codecConfig.newScanType()
					err = row.Scan(outputHstore, &postgresEqual)
					if err != nil {
						t.Fatalf("variant=%s (%#v) queryMode=%s conn=%s: Scan failed: %s",
							hstoreToString(variant), hstoreToString(variant), queryMode.String(), connConfig.codecConfig.name, err)
					}

					// t.Errorf(hstoreToString(variant))
					if !isScannedHstoreEqual(input, outputHstore) {
						t.Errorf("variant=%#v queryMode=%s conn=%s: postgres parsed value does not match input; output=%s",
							hstoreToString(variant), queryMode.String(), connConfig.codecConfig.name, outputHstore)
					}
					if !postgresEqual {
						t.Errorf("variant=%s queryMode=%s conn=%s: postgres did not think the values were equal",
							hstoreToString(variant), queryMode.String(), connConfig.codecConfig.name)
					}
				}
			}
		}
	})
}
