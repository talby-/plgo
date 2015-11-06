package plgo

/*
#cgo CFLAGS: -D_REENTRANT -D_GNU_SOURCE -DDEBIAN -fstack-protector -fno-strict-aliasing -pipe -I/usr/local/include -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64  -I/usr/lib/perl/5.18/CORE
#cgo LDFLAGS: -Wl,-E  -fstack-protector -L/usr/local/lib  -L/usr/lib/perl/5.18/CORE -lperl -ldl -lm -lpthread -lc -lcrypt
#include "glue.h"
*/
import "C"
import (
	"reflect"
	"runtime"
	"unsafe"
)

type pl_t struct {
	thx *C.PerlInterpreter
	err *C.SV
}

type sv_t struct {
	pl *pl_t
	sv *C.SV
}

type SV struct {
	pl *pl_t
	sv *C.SV
}

func New() (self *pl_t) {
	self = new(pl_t)
	self.thx = C.glue_init()
	runtime.SetFinalizer(self, func(dest *pl_t) {
		C.glue_fini(dest.thx)
	})
	return self
}

func (pl *pl_t) Bind(ptr interface{}, defn string) error {
	var esv *C.SV
	sv := C.glue_eval(pl.thx, C.CString(defn), &esv)
	if esv != nil {
		return pl.new(esv)
	}
	if ptr != nil {
		val := reflect.ValueOf(ptr).Elem()
		val.Set(pl.valueOfSV(sv, val.Type()))
	}
	return nil
}

//func (pl *pl_t) NEval(text string) error {
//	var fp func()
//	err := pl.Bind(&fp, text)
//	if err != nil {
//		return err
//	}
//	return fp()
//	if pl.err != nil {
//		return pl.new(pl.err)
//	}
//	return nil
//}

func (pl *pl_t) eval(text string, args []interface{}) (rv *sv_t, err *sv_t) {
	/* to get this to a point it will set up @_ we need to do the exec
	 * in two steps eval("sub { ... }")->(...)
	 * this is somewhat nice because this will let us separate "compile"
	 * and "run" time errors. */
	var e_sv *C.SV
	lst := make([]*C.SV, 1+len(args))
	for i, arg := range args {
		lst[i] = pl.svOfValue(reflect.ValueOf(arg))
	}
	cv := C.glue_eval(pl.thx, C.CString("sub {"+text+"}"), &e_sv)
	if e_sv == nil {
		var out *C.SV
		e_sv = C.glue_call_sv(pl.thx, cv, &lst[0], &out, 1)
		rv = pl.new(out)
	}
	err = pl.new(e_sv)
	return
}

func (pl *pl_t) Eval(text string, args ...interface{}) (*sv_t, *sv_t) {
	return pl.eval(text, args)
}

func (pl *pl_t) MustEval(text string, args ...interface{}) (rv *sv_t) {
	rv, err := pl.eval(text, args)
	if err != nil {
		panic(err)
	}
	return
}

func (pl *pl_t) svOfValue(src reflect.Value) *C.SV {
	switch src.Kind() {
	case reflect.Bool:
		return C.glue_newBool(pl.thx, C.bool(src.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return C.glue_newIV(pl.thx, C.IV(src.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return C.glue_newUV(pl.thx, C.UV(src.Uint()))
	case reflect.Uintptr:
		return C.glue_newUV(pl.thx, C.UV(src.Uint()))
	case reflect.Float32, reflect.Float64:
		return C.glue_newNV(pl.thx, C.NV(src.Float()))
	case reflect.Complex64, reflect.Complex128:
		// TODO: make this actually work
		var fn func(float64, float64) *SV
		pl.Bind(&fn, `sub {
			require Math::Complex;
			my $rv = Math::Complex->new(0, 0);
			$rv->_set_cartesian([ @_ ]);
			return $rv;
		}`)
		v := src.Complex()
		rv := fn(real(v), imag(v))
		return rv.sv
	case reflect.Array,
		reflect.Slice:
		dst := make([]*C.SV, 1+src.Len())
		for i := 0; i < src.Len(); i++ {
			dst[i] = pl.svOfValue(src.Index(i))
		}
		return C.glue_newAV(pl.thx, &dst[0])
	case reflect.Chan:
	case reflect.Func:
		// careful here, the pl_t encapsulation seems delicate
		pv := reflect.ValueOf(pl)
		return C.glue_newCV(pl.thx, unsafe.Pointer(&src), unsafe.Pointer(&pv))
	case reflect.Interface:
	case reflect.Map:
		keys := src.MapKeys()
		dst := make([]*C.SV, 1+2*len(keys))
		for i, key := range keys {
			dst[i<<1] = pl.svOfValue(key)
			dst[i<<1+1] = pl.svOfValue(src.MapIndex(key))
		}
		return C.glue_newHV(pl.thx, &dst[0])
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.SV unwrapping
		var sv *SV
		if src.Type().AssignableTo(reflect.TypeOf(sv)) {
			sv = src.Interface().(*SV)
			return sv.sv
		}
	case reflect.String:
		str := src.String()
		return C.glue_newPV(pl.thx, C.CString(str), C.STRLEN(len(str)))
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + src.Kind().String() + "\"")
	return nil
}

//export glue_stepHV
func glue_stepHV(cb unsafe.Pointer, key *C.SV, val *C.SV) {
	(*(*func(*C.SV, *C.SV))(cb))(key, val)
}

//export glue_stepAV
func glue_stepAV(cb unsafe.Pointer, sv *C.SV) {
	(*(*func(*C.SV))(cb))(sv)
}

func (pl *pl_t) valueOfSV(src *C.SV, ty reflect.Type) reflect.Value {
	switch ty.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(bool(C.glue_getBool(pl.thx, src)))
	case reflect.Int:
		return reflect.ValueOf(int(C.glue_getIV(pl.thx, src)))
	case reflect.Int8:
		return reflect.ValueOf(int8(C.glue_getIV(pl.thx, src)))
	case reflect.Int16:
		return reflect.ValueOf(int16(C.glue_getIV(pl.thx, src)))
	case reflect.Int32:
		return reflect.ValueOf(int32(C.glue_getIV(pl.thx, src)))
	case reflect.Int64:
		return reflect.ValueOf(int64(C.glue_getIV(pl.thx, src)))
	case reflect.Uint:
		return reflect.ValueOf(uint(C.glue_getUV(pl.thx, src)))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(C.glue_getUV(pl.thx, src)))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(C.glue_getUV(pl.thx, src)))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(C.glue_getUV(pl.thx, src)))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(C.glue_getUV(pl.thx, src)))
	case reflect.Uintptr:
		return reflect.ValueOf(uintptr(C.glue_getUV(pl.thx, src)))
	case reflect.Float32:
		return reflect.ValueOf(float32(C.glue_getNV(pl.thx, src)))
	case reflect.Float64:
		return reflect.ValueOf(float64(C.glue_getNV(pl.thx, src)))
	case reflect.Complex64:
		var fn func(*SV) (float32, float32)
		pl.Bind(&fn, `sub {
			require Math::Complex;
			return(Math::Complex::Re($_[0]), Math::Complex::Im($_[0]));
		}`)
		var sv SV
		sv.pl = pl
		sv.sv = src
		re, im := fn(&sv)
		return reflect.ValueOf(complex64(complex(re, im)))
	case reflect.Complex128:
		var fn func(*SV) (float64, float64)
		pl.Bind(&fn, `sub {
			require Math::Complex;
			return(Math::Complex::Re($_[0]), Math::Complex::Im($_[0]));
		}`)
		var sv SV
		sv.pl = pl
		sv.sv = src
		re, im := fn(&sv)
		return reflect.ValueOf(complex(re, im))
	case reflect.Array:
	case reflect.Chan:
	case reflect.Func:
		return reflect.MakeFunc(ty, func(in []reflect.Value) []reflect.Value {
			in_sv := make([]*C.SV, 1+ty.NumIn())
			for i, val := range in {
				in_sv[i] = pl.svOfValue(val)
			}
			out_sv := make([]*C.SV, 1+ty.NumOut())
			err := C.glue_call_sv(pl.thx, src,
				&in_sv[0], &out_sv[0], C.int(ty.NumOut()))
			if err != nil {
				pl.err = err
			}
			out := make([]reflect.Value, ty.NumOut())
			for i, sv := range out_sv[0:len(out)] {
				out[i] = pl.valueOfSV(sv, ty.Out(i))
			}
			return out
		})
	case reflect.Interface:
	case reflect.Map:
		dst := reflect.MakeMap(ty)
		cb := func(key *C.SV, val *C.SV) {
			dst.SetMapIndex(pl.valueOfSV(key, ty.Key()),
				pl.valueOfSV(val, ty.Elem()))
		}
		C.glue_walkHV(pl.thx, src, unsafe.Pointer(&cb))
		return dst
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.SV wrapping
		var sv *SV
		if ty.AssignableTo(reflect.TypeOf(sv)) {
			if !C.glue_SvOK(pl.thx, src) {
				return reflect.ValueOf(nil)
			}
			sv = new(SV)
			sv.pl = pl
			sv.sv = src
			return reflect.ValueOf(sv)
		}
	case reflect.Slice:
		dst := reflect.MakeSlice(ty, 0, 0)
		cb := func(sv *C.SV) {
			dst = reflect.Append(dst, pl.valueOfSV(sv, ty.Elem()))
		}
		C.glue_walkAV(pl.thx, src, unsafe.Pointer(&cb))
		return dst
	case reflect.String:
		var len C.STRLEN
		cs := C.glue_getPV(pl.thx, src, &len)
		return reflect.ValueOf(C.GoStringN(cs, C.int(len)))
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + ty.Kind().String() + "\"")
	return reflect.ValueOf(nil)
}

// the wrap remembers which thx this SV came from
func (pl *pl_t) new(sv *C.SV) (self *sv_t) {
	if sv != nil {
		self = new(sv_t)
		self.pl = pl
		self.sv = sv
		runtime.SetFinalizer(self, func(dest *sv_t) {
			C.glue_dec(dest.pl.thx, dest.sv)
		})
	}
	return
}

func (pl *pl_t) New(val interface{}) *sv_t {
	return pl.new(pl.svOfValue(reflect.ValueOf(val)))
}

func (sv *SV) Dump() {
	C.glue_sv_dump(sv.pl.thx, sv.sv)
}

// an sv_t should have an interface as similar to a reflect.Value as is
// practical to mimic...
func (src *sv_t) Addr() *sv_t {
	return src.pl.new(C.glue_newRV(src.pl.thx, src.sv))
}

func (src *sv_t) Bool() bool {
	return bool(C.glue_getBool(src.pl.thx, src.sv))
}

func (src *sv_t) Bytes() []byte {
	return []byte(src.String())
}

//func (src *sv_t) Call(in []Value) []Value
//func (src *sv_t) Call(in []*sv_t) []*sv_t {
//	var e_sv *C.SV
//	in_sv := make([]*C.SV, 1 + len(in));
//	for i, arg : range in {
//		lst[i] = sv.sv
//	}
//
//}
//

func (s *sv_t) call(name string, args []interface{}) (rv *sv_t, err *sv_t) {
	var e_sv *C.SV
	lst := make([]*C.SV, 2+len(args))
	lst[0] = s.sv
	for i, arg := range args {
		lst[i+1] = s.pl.svOfValue(reflect.ValueOf(arg))
	}
	rv = s.pl.new(C.glue_call_method(s.pl.thx, C.CString(name), &lst[0], &e_sv))
	err = s.pl.new(e_sv)
	return
}

func (s *sv_t) MCall(name string, args ...interface{}) (*sv_t, *sv_t) {
	return s.call(name, args)
}

func (s *sv_t) MustMCall(name string, args ...interface{}) (rv *sv_t) {
	rv, err := s.call(name, args)
	if err != nil {
		panic(err)
	}
	return
}

//func (src *sv_t) CallSlice(in []Value) []Value
//func (src *sv_t) CanAddr() bool
//func (src *sv_t) CanInterface() bool
//func (src *sv_t) CanSet() bool
//func (src *sv_t) Cap() int
//func (src *sv_t) Close()
//func (src *sv_t) Complex() complex128
//func (src *sv_t) Convert(t Type) *sv_t

func (src *sv_t) String() string {
	var len C.STRLEN
	cstr := C.glue_getPV(src.pl.thx, src.sv, &len)
	return C.GoStringN(cstr, C.int(len))
}

func (src *sv_t) Int() int64 {
	return int64(C.glue_getIV(src.pl.thx, src.sv))
}

func (src *sv_t) Uint() uint64 {
	return uint64(C.glue_getUV(src.pl.thx, src.sv))
}

func (src *sv_t) Float() float64 {
	return float64(C.glue_getNV(src.pl.thx, src.sv))
}

func (src *sv_t) Error() string {
	return src.String()
}

//export go_invoke
func go_invoke(call *interface{}, data *interface{}, n int, args_raw unsafe.Pointer) **C.SV {
	// recover the thx wrap
	pl := (*data).(*pl_t)

	// learn about the callback we're about to make
	cb := reflect.ValueOf(*call)
	ty := cb.Type()

	// transform the call arguments
	args_sv := *(*[]*C.SV)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(args_raw),
		Len:  n,
		Cap:  n,
	}))
	args := make([]reflect.Value, n)
	for i, sv := range args_sv {
		args[i] = pl.valueOfSV(sv, ty.In(i))
	}

	// dispatch the call
	rv := cb.Call(args)

	// transform return values
	lst := make([]*C.SV, 1+len(rv))
	for i, val := range rv {
		lst[i] = pl.svOfValue(val)
	}

	// TODO: lst is not properly referenced and could be GC'd
	return &lst[0]
}
