# Faster pgx types

This repository contains types for use with the (pgx Go Postgres driver)[https://github.com/jackc/pgx] that are faster, but are in some way incompatible. It currently only contains two variants of Hstore. They use a single backing string for all key/value pairs, instead of separate strings. This makes it about ~40% faster when parsing values from Postgres, but may have a larger memory footprint if an application holds on to a small number of the keys or values.

* `Hstore`: This is a `map[string]pgtype.Text` instead of `map[string]*string` as used by `pgtype.Hstore`. Since this removes pointers, it requires one fewer allocation per Hstore, and is about ~5% faster when parsing. However, it appears to allocate a bit more memory, I think because the map itself is larger. It is not directly API compatible with `pgtype.Hstore`.
* `HstoreCompat`: This is API compatible with `pgx/pgtype.Hstore` because it uses a `map[string]*string`, but is about ~5% slower because of it.

This code has the same LICENSE as the upstream repository since it basically copied the code then edited it. See the [original upstream pull request discussion for details](https://github.com/jackc/pgx/pull/1645) where it was decided not to make this change upstream.

The only test is a fuzz test, since porting the tests was going to be challenging, and I hope that provides excellent coverage without a ton of work.


## Benchmark results

The results show `Hstore` is about 5% faster than `HstoreCompat`. It uses one less allocation but a bit more memory.

Both these types are quite a bit faster than `pgtype.Hstore` because they share a single backing string.


Detailed results from this repository's benchmark, run with `go test . -bench=. -benchtime=2s`:

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


