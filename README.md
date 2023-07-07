# Faster pgx types

This repository contains types for the [pgx Go Postgres driver](https://github.com/jackc/pgx) that are faster, but have incompatible APIs. It currently contains two variants of `Hstore`. This Hstore implementation uses a single string for all key/value pairs, instead of separate strings. This makes it about ~40% faster when parsing values from Postgres. However, it can have a larger memory footprint, if an application keeps pointers to a subset of the keys/values.

* `Hstore`: This is a `map[string]pgtype.Text` instead of `map[string]*string` as used by `pgtype.Hstore`. Since this removes pointers, it requires one fewer allocation per Hstore, and is about ~5% faster than `HstoreCompat` when parsing. However, it appears to allocate a bit more total memory, I think because the map itself is larger. It is not API compatible with `pgtype.Hstore`.
* `HstoreCompat`: This is API compatible with `pgx/pgtype.Hstore` because it uses a `map[string]*string`, but is about ~5% slower.

This code has the same LICENSE as the upstream repository since it basically copied the code then edited it. See the [original upstream pull request discussion for details](https://github.com/jackc/pgx/pull/1645) where it was decided not to make this change upstream.

The only tests are two fuzz test, since porting the tests was going to be challenging. The fuzz tests included here should provide excellent coverage, without too much code.

## Using this type in your program

To get the best performance, you want to use the binary protocol. To do that, you must register this type:

TODO document


## Benchmark results

Results from this repository's benchmark, run with `go test . -bench=. -benchtime=2s`:

### ARM M1 Max (Macbook Pro 2021)

```
BenchmarkHstoreScan/pgxtypefaster/databasesql.Scan-10         	  237426	     10002 ns/op	   13976 B/op	      34 allocs/op
BenchmarkHstoreScan/fastercompat/databasesql.Scan-10          	  234495	     10189 ns/op	   11960 B/op	      44 allocs/op
BenchmarkHstoreScan/pgtype/databasesql.Scan-10                	  168888	     14179 ns/op	   20592 B/op	     340 allocs/op

BenchmarkHstoreScan/pgxfastertype/text-10                     	  218518	     11130 ns/op	   23087 B/op	      34 allocs/op
BenchmarkHstoreScan/fastercompat/text-10                      	  212636	     11264 ns/op	   21072 B/op	      44 allocs/op
BenchmarkHstoreScan/pgtype/text-10                            	  158030	     15043 ns/op	   29696 B/op	     339 allocs/op

BenchmarkHstoreScan/pgxfastertype/binary-10                   	  341541	      7127 ns/op	   23240 B/op	      31 allocs/op
BenchmarkHstoreScan/fastercompat/binary-10                    	  315151	      7467 ns/op	   21224 B/op	      41 allocs/op
BenchmarkHstoreScan/pgtype/binary-10                          	  229638	     10381 ns/op	   20368 B/op	     316 allocs/op
```
