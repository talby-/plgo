package plgo_test

import (
	"fmt"
	"git.dev.whs/talby/plgo"
	"math"
	"math/cmplx"
	"reflect"
	"testing"
)

var pl = plgo.New()

func ExamplePL_Eval() {
	// get a Perl interpreter
	p := plgo.New()

	// load Perl's SHA package
	p.Eval(`use Digest::SHA`)

	// extract a SHA hash from Perl
	var sum string
	p.Eval(`Digest::SHA::sha1_hex("hello")`, &sum)
	fmt.Println(sum)

	// extract the SHA hashing function from Perl
	var sha1 func(string) string
	p.Eval(`\&Digest::SHA::sha1_hex`, &sha1)
	fmt.Println(sha1("hello"))

	// handle errors from Perl
	var sha1Careful func(string) (string, error)
	p.Eval(`sub {
		die "too short" unless length $_[0] > 0;
		return Digest::SHA::sha1_hex($_[0])
	}`, &sha1Careful)

	_, err := sha1Careful("")
	fmt.Println(err)

	// Output:
	// aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	// aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	// too short at plgo.Eval() line 2.
}

func leak(t *testing.T, n int, obj interface{}, txt string) {
	var inFn, rvFn func()
	switch val := obj.(type) {
	case bool:
		var fn func(bool)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case int:
		var fn func(int)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case uint:
		var fn func(uint)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case float64:
		var fn func(float64)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case complex128:
		var fn func(complex128)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case map[string]int:
		var fn func(map[string]int)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case []int:
		var fn func([]int)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case string:
		var fn func(string)
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	case func():
		var fn func(func())
		pl.Eval(`sub {}`, &fn)
		rvFn = func() { fn(val) }
		inFn = func() { pl.Eval(txt, &val) }
	default:
		t.Errorf("unsupported type for leak check")
		return
	}
	a := pl.Live()
	for i := 0; i < n; i++ {
		rvFn()
	}
	b := pl.Live()
	if n <= b-a {
		t.Errorf("leak: rv %d SVs over %d calls\n", b-a, n)
	}
	for i := 0; i < n; i++ {
		inFn()
	}
	c := pl.Live()
	if n <= c-b {
		t.Errorf("leak: in %d SVs over %d calls\n", c-b, n)
	}
}

func TestEval(t *testing.T) {
	err := pl.Eval(`1`)
	if err != nil {
		t.Errorf("perl error unexpected: %s", err.Error())
	}
}

func TestErr(t *testing.T) {
	err := pl.Eval(`1 = 2`)
	if err == nil {
		t.Errorf("pl.Eval() error expected")
	}
	var f func() error
	pl.Eval(`sub { die "tippy\n" }`, &f)
	err = f()
	if err == nil || err.Error() != "tippy\n" {
		t.Errorf("perl error expected")
	}

	var g func() int
	pl.Eval(`sub { die "tippy\n" }`, &g)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("perl panic expected\n")
			}
		}()
		g()
	}()
}

func TestBool(t *testing.T) {
	var id func(bool) bool
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want bool) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v bool) => %v", want, have)
		}
	}
	is := func(expr string, want bool) {
		var have bool
		pl.Eval(expr, &have)
		if want != have {
			t.Errorf("is(`%s`) want %v have %v", expr, want, have)
		}
	}
	is(`undef`, false)
	is(`1 == 1`, true)
	is(`1 == 0`, false)
	is(`''`, false)
	is(`'0'`, false)
	is(`'1'`, true)
	is(`'2'`, true)
	is(`'-1'`, true)
	is(`'a string'`, true)
	ok(true)
	ok(false)

	leak(t, 1024, true, `1`)
}

func TestInt(t *testing.T) {
	var id func(int) int
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want int) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v int) => %v", want, have)
		}
	}
	// the capacity of an "int" is system dependent
	ok(-1)
	ok(0)
	ok(1)

	leak(t, 1024, 12, `21`)
}

func TestInt8(t *testing.T) {
	var id func(int8) int8
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want int8) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v int8) => %v", want, have)
		}
	}
	ok(-128)
	ok(-1)
	ok(0)
	ok(1)
	ok(127)
}

func TestInt16(t *testing.T) {
	var id func(int16) int16
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want int16) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v int16) => %v", want, have)
		}
	}
	ok(-32768)
	ok(-1)
	ok(0)
	ok(1)
	ok(32767)
}

func TestInt32(t *testing.T) {
	var id func(int32) int32
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want int32) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v int32) => %v", want, have)
		}
	}
	ok(-2147483648)
	ok(-1)
	ok(0)
	ok(1)
	ok(2147483647)
}

func TestInt64(t *testing.T) {
	var id func(int64) int64
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want int64) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v int64) => %v", want, have)
		}
	}
	ok(-9223372036854775808)
	ok(-1)
	ok(0)
	ok(1)
	ok(9223372036854775807)
}

func TestUint(t *testing.T) {
	var id func(uint) uint
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uint) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uint) => %v", want, have)
		}
	}
	// the capacity of a "uint" is system dependent
	ok(0)
	ok(1)

	leak(t, 1024, uint(12), `21`)
}

func TestUint8(t *testing.T) {
	var id func(uint8) uint8
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uint8) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uint8) => %v", want, have)
		}
	}
	ok(0)
	ok(1)
	ok(255)
}

func TestUint16(t *testing.T) {
	var id func(uint16) uint16
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uint16) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uint16) => %v", want, have)
		}
	}
	ok(0)
	ok(1)
	ok(65535)
}

func TestUint32(t *testing.T) {
	var id func(uint32) uint32
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uint32) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uint32) => %v", want, have)
		}
	}
	ok(0)
	ok(1)
	ok(4294967295)
}

func TestUint64(t *testing.T) {
	var id func(uint64) uint64
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uint64) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uint64) => %v", want, have)
		}
	}
	ok(0)
	ok(1)
	ok(18446744073709551615)
}

func TestUintptr(t *testing.T) {
	var id func(uintptr) uintptr
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want uintptr) {
		have := id(want)
		if want != have {
			t.Errorf("id(%v uintptr) => %v", want, have)
		}
	}
	ok(uintptr(0))
	ok(uintptr(1))
	ok(^uintptr(0))
}

// matching floating point numbers exactly is always a little sketchy.
func TestFloat32(t *testing.T) {
	var id func(float32) float32
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want float32) {
		have := id(want)
		if want != have &&
			!(math.IsNaN(float64(want)) && math.IsNaN(float64(have))) {
			t.Errorf("id(%v float32) => %v", want, have)
		}
	}
	ok(float32(math.Inf(-1)))
	ok(-math.MaxFloat32)
	ok(-1.0)
	ok(-math.SmallestNonzeroFloat32)
	ok(0.0)
	ok(math.SmallestNonzeroFloat32)
	ok(1.0)
	ok(math.MaxFloat32)
	ok(float32(math.Inf(1)))
	ok(float32(math.NaN()))
}

func TestFloat64(t *testing.T) {
	var id func(float64) float64
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want float64) {
		have := id(want)
		if want != have &&
			!(math.IsNaN(want) && math.IsNaN(have)) {
			t.Errorf("id(%v float64) => %v", want, have)
		}
	}
	ok(math.Inf(-1))
	ok(-math.MaxFloat64)
	ok(-1.0)
	ok(-math.SmallestNonzeroFloat64)
	ok(0.0)
	ok(math.SmallestNonzeroFloat64)
	ok(1.0)
	ok(math.MaxFloat64)
	ok(math.Inf(1))
	ok(math.NaN())

	leak(t, 1024, 12.2, `21.1`)
}

func TestComplex64(t *testing.T) {
	var id func(complex64) complex64
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want complex64) {
		have := id(want)
		if want == have ||
			(cmplx.IsNaN(complex128(want)) && cmplx.IsNaN(complex128(have))) ||
			(cmplx.IsInf(complex128(want)) && cmplx.IsInf(complex128(have))) {
			return
		}
		t.Errorf("id(%v complex64) => %v", want, have)
	}
	vals := []float32{
		float32(math.Inf(-1)),
		-math.MaxFloat32,
		-1.0,
		-math.SmallestNonzeroFloat32,
		0.0,
		math.SmallestNonzeroFloat32,
		1.0,
		math.MaxFloat32,
		float32(math.Inf(1)),
		float32(math.NaN()),
	}
	for _, re := range vals {
		for _, im := range vals {
			ok(complex(re, im))
		}
	}
	ok(complex64(-cmplx.Inf()))
	ok(complex64(cmplx.Inf()))
	ok(complex64(cmplx.NaN()))
}

func TestComplex128(t *testing.T) {
	var id func(complex128) complex128
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want complex128) {
		have := id(want)
		if want == have ||
			(cmplx.IsNaN(want) && cmplx.IsNaN(have)) ||
			(cmplx.IsInf(want) && cmplx.IsInf(have)) {
			return
		}
		t.Errorf("id(%v complex128) => %v", want, have)
	}
	vals := []float64{
		math.Inf(-1),
		-math.MaxFloat64,
		-1.0,
		-math.SmallestNonzeroFloat64,
		0.0,
		math.SmallestNonzeroFloat64,
		1.0,
		math.MaxFloat64,
		math.Inf(1),
		math.NaN(),
	}
	for _, re := range vals {
		for _, im := range vals {
			ok(complex(re, im))
		}
	}
	ok(-cmplx.Inf())
	ok(cmplx.Inf())
	ok(cmplx.NaN())

	leak(t, 1024, 12.2i, `Math::Complex->new(0, 21.1)`)
}

func TestFunc(t *testing.T) {
	var id func(func(int) int) func(int) int
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want func(int) int) {
		have := id(want)
		if have(18) != want(18) {
			t.Errorf("id(%v func() int) => %v", want, have)
		}
	}
	ok(func(v int) int {
		return v + 54321
	})

	// GC on Go closures appears to be unpredictable
	//leak(t, 1024, func() { }, `sub {}`)
}

func TestMap(t *testing.T) {
	var id func(map[string]int) map[string]int
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want map[string]int) {
		have := id(want)
		if !reflect.DeepEqual(have, want) {
			t.Errorf("id(%v map[string]int) => %v", want, have)
		}
	}
	ok(map[string]int{
		"fun": 12,
		"jam": 8,
	})

	leak(t, 1024, map[string]int{"uuu": 17}, `{ vvv => 18 }`)
}

/*
func TestSV(t *testing.T) {
	ok := func(mk, ck string) {
		var val *plgo.sV
		var chk func(*plgo.sV) bool
		pl.Eval(mk, &val)
		pl.Eval("sub {"+ck+"}", &chk)
		if !chk(val) {
			t.Errorf("sub { %s }->(%s) fail", ck, mk)
		}
	}
	ok(`321`, `$_[0] == 321`)                              // IV
	ok(`32.1`, `$_[0] == 32.1`)                            // NV
	ok(`$val = 15; \$val`, `$_[0] == \$val`)               // RV
	ok(`'a string'`, `$_[0] eq 'a string'`)                // PV
	ok(`$^T`, `$_[0] == $^T`)                              // PVMG
	ok(`qr/fun/`, `"funny" =~ $_[0] and "jello" !~ $_[0]`) // REGEXP
	ok(`[ 1 .. 3 ]`, `"@{$_[0]}" eq '1 2 3'`)              // PVAV
	ok(`{ a => 'b' }`, `"@{[ %{$_[0]} ]}" eq 'a b'`)       // PVHV
	ok(`sub { 543 }`, `$_[0]->() == 543`)                  // PVCV
	ok(`undef`, `not defined $_[0]`)
}
*/

func TestSlice(t *testing.T) {
	var id func([]int) []int
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want []int) {
		have := id(want)
		if !reflect.DeepEqual(have, want) {
			t.Errorf("id(%v []int) => %v", want, have)
		}
	}
	ok([]int{})
	ok([]int{1, 2, 3})

	leak(t, 1024, []int{17, 18}, `[ 19, 20 ]`)
}

func TestString(t *testing.T) {
	var id func(string) string
	pl.Eval(`sub { $_[0] }`, &id)
	ok := func(want string) {
		have := id(want)
		if !reflect.DeepEqual(have, want) {
			t.Errorf("id(%v string) => %v", want, have)
		}
	}
	ok("")
	ok("a string")

	leak(t, 1024, "uuu", `"vvv"`)
}

func TestMulti(t *testing.T) {
	var fn func(int, int) (int, int, int, int, int)
	pl.Eval(`sub {
		use strict;
		use warnings;
		# the xgcd algorithm
		my($u, $v) = @_;
		my($s, $old_s) = (0, 1);
		my($t, $old_t) = (1, 0);
		my($r, $old_r) = ($v, $u);
		while($r) {
			my $quotient = int($old_r / $r);
			($old_r, $r) = ($r, $old_r - $quotient * $r);
			($old_s, $s) = ($s, $old_s - $quotient * $s);
			($old_t, $t) = ($t, $old_t - $quotient * $t);
		}
		return($old_s, $u, 0 - $old_t, $v, $old_r) if $old_s > 0;
		return(0 - $old_s, $u, $old_t, $v, $old_r);
	}`, &fn)
	a, b, c, d, e := fn(12345, 54321)
	if a != 3617 || b != 12345 || c != 822 || d != 54321 || e != 3 {
		t.Errorf("%d*%d-%d*%d ?= %d", a, b, c, d, e)
	}
}

func BenchmarkEval(b *testing.B) {
	var v int
	for i := 0; i < b.N; i++ {
		pl.Eval(`123`, &v)
		if v != 123 {
			panic("ugh")
		}
	}
}

func BenchmarkCall(b *testing.B) {
	var fn func() int
	pl.Eval(`sub () { 123 }`, &fn)
	for i := 0; i < b.N; i++ {
		if fn() != 123 {
			panic("ugh")
		}
	}
}
