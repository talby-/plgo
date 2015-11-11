package plgo

//go:generate ./gen.pl $GOFILE

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

var active_pl *pl_t

type SV struct {
	pl  *pl_t
	sv  *C.SV
	own bool
}

// We can not reliably hold pointers to Go objects in C
// https://github.com/golang/go/issues/12416 documents the rules.
// runtime.GC() can move objects in memory so we have to create an
// indirection layer.  The live_vals map will serve this purpose.
var live_vals_n = 0
var live_vals = map[int]*reflect.Value{}

func New() (self *pl_t) {
	self = new(pl_t)
	self.thx = C.glue_init()
	runtime.SetFinalizer(self, func(dest *pl_t) {
		C.glue_fini(dest.thx)
	})
	return self
}

func (pl *pl_t) Leak(f func()) int {
	runtime.GC()
	a := C.glue_count_live(pl.thx)
	f()
	runtime.GC()
	b := C.glue_count_live(pl.thx)
	return int(b - a)
}

func (pl *pl_t) Bind(ptr interface{}, defn string) error {
	var err *C.SV
	prev_pl := active_pl
	active_pl = pl
	sv := C.glue_eval(pl.thx, C.CString(defn), &err)
	active_pl = prev_pl
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
		live_vals_n++
		id := live_vals_n
		live_vals[id] = &src
		return C.glue_newCV(pl.thx, C.IV(id), C.IV(t.NumIn()), C.IV(t.NumOut()))
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
		re, im := pl.valSVcmplx(pl.sV(src, false))
		dst.SetComplex(complex128(complex(re, im)))
		return
	case reflect.Array:
	case reflect.Chan:
	case reflect.Func:
		var cv *SV
		dst.Set(reflect.MakeFunc(t, func(arg []reflect.Value) (ret []reflect.Value) {
			args := make([]*C.SV, 1+t.NumIn())
			for i, val := range arg {
				args[i] = pl.newSVval(val)
			}
			rets := make([]*C.SV, 1+t.NumOut())

			prev_pl := active_pl
			active_pl = pl
			C.glue_call_sv(pl.thx, cv.sv,
				&args[0], &rets[0], C.int(t.NumOut()))
			active_pl = prev_pl

			ret = make([]reflect.Value, t.NumOut())
			for i, sv := range rets[0:len(ret)] {
				ret[i] = reflect.New(t.Out(i)).Elem()
				pl.valSV(&ret[i], sv)
				C.glue_dec(pl.thx, sv)
			}
			return
		}))
		cv = pl.sV(src, false)
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
			sv = pl.sV(src, false)
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
	if sv.own {
		C.glue_dec(sv.pl.thx, sv.sv)
	}
}

func (pl *pl_t) sV(sv *C.SV, own bool) *SV {
	var self SV
	self.pl = pl
	self.sv = sv
	self.own = own
	C.glue_inc(pl.thx, sv)
	runtime.SetFinalizer(&self, svFini)
	return &self
}

func (self *SV) Error() string {
	var s string
	v := reflect.ValueOf(s)
	self.pl.valSV(&v, self.sv)
	return s
}

//export go_invoke
func go_invoke(data int, arg unsafe.Pointer, ret unsafe.Pointer) {
	if active_pl == nil {
		panic("active_pl not set")
	}
	cnv := func(raw unsafe.Pointer, n int) []*C.SV {
		return *(*[]*C.SV)(unsafe.Pointer(&reflect.SliceHeader{
			Data: uintptr(raw),
			Len:  n,
			Cap:  n,
		}))
	}
	// recover info
	val := live_vals[data]
	t := val.Type()
	// xlate args - they are already mortal, don't take ownership unless
	// they need to survive beyond the function call
	args := make([]reflect.Value, t.NumIn())
	for i, sv := range cnv(arg, len(args)) {
		args[i] = reflect.New(t.In(i)).Elem()
		active_pl.valSV(&args[i], sv)
	}
	// xlate rets - return as owning references and glue_invoke() will
	// mortalize them for us
	rets := cnv(ret, t.NumOut())
	for i, val := range val.Call(args) {
		rets[i] = active_pl.newSVval(val)
	}
}

//export go_release
func go_release(data int) {
	delete(live_vals, data)
}
