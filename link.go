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
	thx        *C.PerlInterpreter
	newSVcmplx func(float64, float64) *SV
	valSVcmplx func(*SV) (float64, float64)
}

type SV struct {
	pl *pl_t
	sv *C.SV
}

// entry for the live_vals table
type ref_t struct {
	pl  *pl_t
	val *reflect.Value
}

var live_vals map[uintptr]*ref_t = map[uintptr]*ref_t{}

func New() (self *pl_t) {
	self = new(pl_t)
	self.thx = C.glue_init()
	runtime.SetFinalizer(self, func(dest *pl_t) {
		C.glue_fini(dest.thx)
	})
	return self
}

func (pl *pl_t) Leak(f func()) int {
	//var t int
	//runtime.GC(); runtime.GC(); runtime.GC(); runtime.GC(); runtime.GC()
	a := C.glue_count_live(pl.thx)
	//pl.Bind(&t, `require Devel::Leak; Devel::Leak::NoteSV($handle)`)
	//a := t
	f()
	//runtime.GC(); runtime.GC(); runtime.GC(); runtime.GC(); runtime.GC()
	b := C.glue_count_live(pl.thx)
	//pl.Bind(&t, `Devel::Leak::CheckSV($handle)`)
	//b := t
	//C.glue_dump_live(pl.thx)
	return int(b - a)
}

func (pl *pl_t) Bind(ptr interface{}, defn string) error {
	var err *C.SV
	sv := C.glue_eval(pl.thx, C.CString(defn), &err)
	if sv == nil {
		panic("glue_eval() => nil?")
	}
	if ptr != nil {
		val := reflect.ValueOf(ptr).Elem()
		pl.valSV(&val, sv)
	}
	C.glue_dec(pl.thx, sv)
	return nil
}

func (pl *pl_t) newSVval(src reflect.Value) *C.SV {
	t := src.Type()
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
		if pl.newSVcmplx == nil {
			pl.Bind(&pl.newSVcmplx, `
				require Math::Complex;
				sub { my $rv = Math::Complex->new(0, 0); $rv->_set_cartesian([ @_ ]); return $rv; }
			`)
		}
		v := src.Complex()
		return pl.newSVcmplx(real(v), imag(v)).sv
	case reflect.Array,
		reflect.Slice:
		dst := make([]*C.SV, 1+src.Len())
		for i := 0; i < src.Len(); i++ {
			dst[i] = pl.newSVval(src.Index(i))
		}
		return C.glue_newAV(pl.thx, &dst[0])
	case reflect.Chan:
	case reflect.Func:
		cb := new(ref_t)
		cb.val = &src
		cb.pl = pl
		ptr := uintptr(unsafe.Pointer(cb))
		live_vals[ptr] = cb
		return C.glue_newCV(pl.thx, C.IV(ptr), C.IV(t.NumIn()), C.IV(t.NumOut()))
	case reflect.Interface:
	case reflect.Map:
		keys := src.MapKeys()
		dst := make([]*C.SV, 1+2*len(keys))
		for i, key := range keys {
			dst[i<<1] = pl.newSVval(key)
			dst[i<<1+1] = pl.newSVval(src.MapIndex(key))
		}
		return C.glue_newHV(pl.thx, &dst[0])
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.SV unwrapping
		var sv *SV
		if t.AssignableTo(reflect.TypeOf(sv)) {
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
func glue_stepHV(cb uintptr, key *C.SV, val *C.SV) {
	(*(*func(*C.SV, *C.SV))(unsafe.Pointer(cb)))(key, val)
}

//export glue_stepAV
func glue_stepAV(cb uintptr, sv *C.SV) {
	(*(*func(*C.SV))(unsafe.Pointer(cb)))(sv)
}

func (pl *pl_t) valSV(dst *reflect.Value, src *C.SV) {
	t := dst.Type()
	switch t.Kind() {
	case reflect.Bool:
		dst.SetBool(bool(C.glue_getBool(pl.thx, src)))
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		dst.SetInt(int64(C.glue_getIV(pl.thx, src)))
		return
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		dst.SetUint(uint64(C.glue_getUV(pl.thx, src)))
		return
	case reflect.Uintptr:
		dst.SetUint(uint64(C.glue_getUV(pl.thx, src)))
		return
	case reflect.Float32, reflect.Float64:
		dst.SetFloat(float64(C.glue_getNV(pl.thx, src)))
		return
	case reflect.Complex64, reflect.Complex128:
		if pl.valSVcmplx == nil {
			pl.Bind(&pl.valSVcmplx, `
				require Math::Complex;
				sub { return Math::Complex::Re($_[0]), Math::Complex::Im($_[0]); }
			`)
		}
		re, im := pl.valSVcmplx(pl.sV(src))
		dst.SetComplex(complex128(complex(re, im)))
		return
	case reflect.Array:
	case reflect.Chan:
	case reflect.Func:
		var cv *SV
		dst.Set(reflect.MakeFunc(t, func(in []reflect.Value) []reflect.Value {
			in_sv := make([]*C.SV, 1+t.NumIn())
			for i, val := range in {
				in_sv[i] = pl.newSVval(val)
			}
			out_sv := make([]*C.SV, 1+t.NumOut())
			C.glue_call_sv(pl.thx, cv.sv,
				&in_sv[0], &out_sv[0], C.int(t.NumOut()))
			out := make([]reflect.Value, t.NumOut())
			for i, sv := range out_sv[0:len(out)] {
				out[i] = reflect.New(t.Out(i)).Elem()
				pl.valSV(&out[i], sv)
				C.glue_dec(pl.thx, sv)
			}
			return out
		}))
		cv = pl.sV(src)
		return
	case reflect.Interface:
	case reflect.Map:
		dst.Set(reflect.MakeMap(t))
		cb := func(key *C.SV, val *C.SV) {
			k := reflect.New(t.Key()).Elem()
			pl.valSV(&k, key)
			v := reflect.New(t.Elem()).Elem()
			pl.valSV(&v, val)
			dst.SetMapIndex(k, v)
		}
		C.glue_walkHV(pl.thx, src, C.IV(uintptr(unsafe.Pointer(&cb))))
		return
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.SV wrapping
		// TODO: this is sketchy, refactor
		var sv *SV
		if dst.Type().AssignableTo(reflect.TypeOf(sv)) {
			sv = pl.sV(src)
			dst.Set(reflect.ValueOf(sv))
			return
		}
	case reflect.Slice:
		// TODO: this is sketchy, refactor
		tmp := reflect.MakeSlice(t, 0, 0)
		cb := func(sv *C.SV) {
			val := reflect.New(t.Elem()).Elem()
			pl.valSV(&val, sv)
			tmp = reflect.Append(tmp, val)
		}
		C.glue_walkAV(pl.thx, src, C.IV(uintptr(unsafe.Pointer(&cb))))
		dst.Set(tmp)
		return
	case reflect.String:
		var len C.STRLEN
		cs := C.glue_getPV(pl.thx, src, &len)
		dst.SetString(C.GoStringN(cs, C.int(len)))
		return
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + t.Kind().String() + "\"")
}

func svFini(sv *SV) {
	C.glue_dec(sv.pl.thx, sv.sv)
}

func (pl *pl_t) sV(sv *C.SV) *SV {
	var self SV
	self.pl = pl
	self.sv = sv
	C.glue_inc(pl.thx, sv)
	//runtime.SetFinalizer(&self, svFini)
	return &self
}

func (self *SV) Error() string {
	var s string
	v := reflect.ValueOf(s)
	self.pl.valSV(&v, self.sv)
	return s
}

//export go_invoke
func go_invoke(data uintptr, arg unsafe.Pointer, ret unsafe.Pointer) {
	// helper
	cnv := func(raw unsafe.Pointer, n int) []*C.SV {
		return *(*[]*C.SV)(unsafe.Pointer(&reflect.SliceHeader{
			Data: uintptr(raw),
			Len:  n,
			Cap:  n,
		}))
	}
	// recover info
	cb := live_vals[data]
	t := cb.val.Type()
	// xlate args - they are already mortal, don't take ownership unless
	// they need to survive beyond the function call
	args := make([]reflect.Value, t.NumIn())
	for i, sv := range cnv(arg, len(args)) {
		args[i] = reflect.New(t.In(i)).Elem()
		cb.pl.valSV(&args[i], sv)
	}
	// xlate rets - return as owning references and glue_invoke() will
	// mortalize them for us
	ret_sv := cnv(ret, t.NumOut())
	rets := cb.val.Call(args)
	for i, val := range rets {
		ret_sv[i] = cb.pl.newSVval(val)
	}
}

//export go_release
func go_release(ptr uintptr) {
	delete(live_vals, ptr)
}
