# Faster pgx types

This repository contains types for use with the (pgx Go Postgres driver)[https://github.com/jackc/pgx] that are faster, but are in some way incompatible. It currently contains two types:

* `Hstore`: The [Postgres Hstore column type](https://www.postgresql.org/docs/current/hstore.html) that is a `map[string]pgtype.Text` instead of `map[string]*string`. This removes pointers from the `map[string]*string` version, so is more efficient, but is not API compatible. This shares a single backing `string` for key/value pairs, so it may also have different garbage collection behaviour from 
* `HstoreCompat`: This is drop-in compatible with `pgx/pgtype.Hstore`, *except* it shares a single `string` for all key/value pairs, so it can only be garbage collected once *all* key/value pairs are unused.

This code has the same LICENSE as the upstream repository since it basically copied the code then edited it. See the [original upstream pull request discussion for details](https://github.com/jackc/pgx/pull/1645) where it was decided not to make this change upstream.

TODO: I did not correctly port the tests. Fix this!


## Benchmark results

From this repository's benchmark, run with `go test -bench=. -benchtime=2s`:

### ARM M1 Max (Macbook Pro 2021)

TODO
