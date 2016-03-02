# plgo
Run Perl code from Go

Actually, to be a bit more precise, this package extracts values from Perl into Go.  This is a slightly uncommon appoach to language embedding driven by [reflect](https://golang.org/pkg/reflect/).  Here's a starter

    // get a Perl interpreter
    p := plgo.New()
    p.Preamble = `use strict; use warnings;`

    // load Perl's SHA package
    p.Eval(`use Digest::SHA`)

after which we can get simple values like strings with:

    // extract a SHA hash from Perl
    var sum string
    p.Eval(`Digest::SHA::sha1_hex("hello")`, &sum)
    fmt.Println(sum)

or since both languages have first class functions:

    // extract the SHA hashing function from Perl
    var sha1 func(string) string
    p.Eval(`\&Digest::SHA::sha1_hex`, &sha1)
    fmt.Println(sha1("hello"))

### Notes
 * This package requires Go 1.6+
 * After downloading, you may need to run `go generate` to resolve libperl compile/link flags.
