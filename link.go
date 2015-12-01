// Package plgo provides a Perl runtime to Go
package plgo

//go:generate ./gen.pl $GOFILE

/*
#cgo CFLAGS: -Wall -D_REENTRANT -D_GNU_SOURCE -DDEBIAN -fstack-protector -fno-strict-aliasing -pipe -I/usr/local/include -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64  -I/usr/lib/perl/5.18/CORE
#cgo LDFLAGS: -Wl,-E  -fstack-protector -L/usr/local/lib  -L/usr/lib/perl/5.18/CORE -lperl -ldl -lm -lpthread -lc -lcrypt
#include "glue.h"
*/
import "C"
import (
	"fmt"
	"reflect"
	"runtime"
	"unsafe"
)

// PL holds a Perl runtime
type PL struct {
	thx        C.tTHX
	cx         chan bool
	newSVcmplx func(float64, float64) *sV
	valSVcmplx func(*sV) (float64, float64)
}

type sV struct {
	pl  *PL
	sv  *C.SV
	own bool
}

// We can not reliably hold pointers to Go objects in C
// https://github.com/golang/go/issues/12416 documents the rules.
// runtime.GC() can move objects in memory so we have to create an
// indirection layer.  The liveCB map will serve this purpose.
var (
	liveCBSeq = 0
	liveCB    = map[int]func(unsafe.Pointer, unsafe.Pointer){}
)

func plFini(pl *PL) {
	pl.sync(func() { C.glue_fini(pl.thx) })
}

// New initializes a Perl runtime
func New() *PL {
	pl := new(PL)
	pl.thx = C.glue_init()
	pl.cx = make(chan bool, 1)
	pl.cx <- true
	runtime.SetFinalizer(pl, plFini)
	return pl
}

func (pl *PL) sync(f func()) {
	<-pl.cx
	f()
	pl.cx <- true
}

func (pl *PL) unsync(f func()) {
	pl.cx <- true
	f()
	<-pl.cx
}

// Eval will execute a string of Perl code.  If ptrs are provided,
// the list of results from Perl will be stored in the list of ptrs.
// Not all types are supported, but many basic types are, including
// functions.
func (pl *PL) Eval(text string, ptrs ...interface{}) error {
	var err, av *C.SV
	code := C.CString("[ do { \n#line 1 \"plgo.Eval()\"\n" + text + "\n } ]")
	pl.sync(func() { av = C.glue_eval(pl.thx, code, &err) })

	if err != nil {
		e := pl.sV(err, true)
		pl.sync(func() {
			C.glue_dec(pl.thx, av)
			C.glue_dec(pl.thx, err)
		})
		return e
	}
	if len(ptrs) > 0 {
		i := 0
		cb := func(sv *C.SV) {
			pl.unsync(func() {
				if i >= len(ptrs) {
					return
				}
				val := reflect.ValueOf(ptrs[i]).Elem()
				pl.valSV(&val, sv)
				i++
			})
		}
		ptr := C.IV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkAV(pl.thx, av, ptr) })
	}
	pl.sync(func() { C.glue_dec(pl.thx, av) })
	return nil
}

// Live counts the number of live variables in the Perl instance.
// This function is used for leak detection in the test code.
// runtime.GC() must be called to get accurate live value counts.
func (pl *PL) Live() int {
	var rv C.IV
	pl.sync(func() { rv = C.glue_count_live(pl.thx) })
	return int(rv)
}

func (pl *PL) newSVval(src reflect.Value) (dst *C.SV) {
	t := src.Type()
	switch src.Kind() {
	case reflect.Bool:
		val := C.bool(src.Bool())
		pl.sync(func() { dst = C.glue_newBool(pl.thx, val) })
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val := C.IV(src.Int())
		pl.sync(func() { dst = C.glue_newIV(pl.thx, val) })
		return
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		val := C.UV(src.Uint())
		pl.sync(func() { dst = C.glue_newUV(pl.thx, val) })
		return
	case reflect.Float32, reflect.Float64:
		val := C.NV(src.Float())
		pl.sync(func() { dst = C.glue_newNV(pl.thx, val) })
		return
	case reflect.Complex64, reflect.Complex128:
		if pl.newSVcmplx == nil {
			pl.Eval(`
				require Math::Complex;
				sub { my $rv = Math::Complex->new(0, 0); $rv->_set_cartesian([ @_ ]); return $rv; }
			`, &pl.newSVcmplx)
		}
		v := src.Complex()
		return pl.newSVcmplx(real(v), imag(v)).sv
	case reflect.Array,
		reflect.Slice:
		lst := make([]*C.SV, 1+src.Len())
		for i := 0; i < src.Len(); i++ {
			lst[i] = pl.newSVval(src.Index(i))
		}
		pl.sync(func() { dst = C.glue_newAV(pl.thx, &lst[0]) })
		return
	case reflect.Chan:
	case reflect.Func:
		liveCBSeq++
		id := C.IV(liveCBSeq)
		liveCB[liveCBSeq] = func(arg unsafe.Pointer, ret unsafe.Pointer) {
			pl.unsync(func() {
				cnv := func(raw unsafe.Pointer, n int) []*C.SV {
					return *(*[]*C.SV)(unsafe.Pointer(&reflect.SliceHeader{
						Data: uintptr(raw),
						Len:  n,
						Cap:  n,
					}))
				}
				// xlate args - they are already mortal, don't take
				// ownership unless they need to survive beyond the
				// function call
				args := make([]reflect.Value, t.NumIn())
				for i, sv := range cnv(arg, len(args)) {
					args[i] = reflect.New(t.In(i)).Elem()
					pl.valSV(&args[i], sv)
				}
				// xlate rets - return as owning references and
				// glue_invoke() will mortalize them for us
				rets := cnv(ret, t.NumOut())
				for i, val := range src.Call(args) {
					rets[i] = pl.newSVval(val)
				}
			})
		}
		ni := C.IV(t.NumIn())
		no := C.IV(t.NumOut())
		pl.sync(func() { dst = C.glue_newCV(pl.thx, id, ni, no) })
		return
	case reflect.Interface:
	case reflect.Map:
		keys := src.MapKeys()
		lst := make([]*C.SV, 1+2*len(keys))
		for i, key := range keys {
			lst[i<<1] = pl.newSVval(key)
			lst[i<<1+1] = pl.newSVval(src.MapIndex(key))
		}
		pl.sync(func() { dst = C.glue_newHV(pl.thx, &lst[0]) })
		return
	case reflect.Ptr:
		// TODO: *sV handling is a special case, but generic Ptr support
		// could be implemented
		if t == reflect.TypeOf((*sV)(nil)) {
			return src.Interface().(*sV).sv
		}
	case reflect.String:
		str := src.String()
		cs := C.CString(str)
		cl := C.STRLEN(len(str))
		pl.sync(func() { dst = C.glue_newPV(pl.thx, cs, cl) })
		return
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + src.Kind().String() + "\"")
}

//export goStepHV
func goStepHV(cb uintptr, key *C.SV, val *C.SV) {
	(*(*func(*C.SV, *C.SV))(unsafe.Pointer(cb)))(key, val)
}

//export goStepAV
func goStepAV(cb uintptr, sv *C.SV) {
	(*(*func(*C.SV))(unsafe.Pointer(cb)))(sv)
}

func (pl *PL) valSV(dst *reflect.Value, src *C.SV) {
	t := dst.Type()
	switch t.Kind() {
	case reflect.Bool:
		var val C.bool
		pl.sync(func() { val = C.glue_getBool(pl.thx, src) })
		dst.SetBool(bool(val))
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var val C.IV
		pl.sync(func() { val = C.glue_getIV(pl.thx, src) })
		dst.SetInt(int64(val))
		return
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var val C.UV
		pl.sync(func() { val = C.glue_getUV(pl.thx, src) })
		dst.SetUint(uint64(val))
		return
	case reflect.Uintptr:
		var val C.UV
		pl.sync(func() { val = C.glue_getUV(pl.thx, src) })
		dst.SetUint(uint64(val))
		return
	case reflect.Float32, reflect.Float64:
		var val C.NV
		pl.sync(func() { val = C.glue_getNV(pl.thx, src) })
		dst.SetFloat(float64(val))
		return
	case reflect.Complex64, reflect.Complex128:
		if pl.valSVcmplx == nil {
			pl.Eval(`
				require Math::Complex;
				sub { return Math::Complex::Re($_[0]), Math::Complex::Im($_[0]); }
			`, &pl.valSVcmplx)
		}
		re, im := pl.valSVcmplx(pl.sV(src, false))
		dst.SetComplex(complex128(complex(re, im)))
		return
	case reflect.Array:
	case reflect.Chan:
	case reflect.Func:
		var cv *sV
		dst.Set(reflect.MakeFunc(t, func(arg []reflect.Value) (ret []reflect.Value) {
			args := make([]*C.SV, 1+t.NumIn())
			for i, val := range arg {
				args[i] = pl.newSVval(val)
			}
			rets := make([]*C.SV, 1+t.NumOut())

			var err *C.SV
			no := C.int(t.NumOut())
			pl.sync(func() { err = C.glue_call_sv(pl.thx, cv.sv, &args[0], &rets[0], no) })

			ret = make([]reflect.Value, t.NumOut())

			var pErr *sV
			if err == nil {
				// copy Perl rets to Go, zero out error rvs
				j := 0
				for i := range ret {
					if t.Out(i) == reflect.TypeOf((*error)(nil)).Elem() {
						ret[i] = reflect.Zero(t.Out(i))
					} else {
						ret[i] = reflect.New(t.Out(i)).Elem()
						pl.valSV(&ret[i], rets[j])
						j++
					}
				}
			} else {
				// copy Perl error to Go, zero out data rvs
				shouldPanic := true
				for i := range ret {
					if t.Out(i) == reflect.TypeOf((*error)(nil)).Elem() {
						ret[i] = reflect.New(t.Out(i)).Elem()
						pl.valSV(&ret[i], err)
						shouldPanic = false
					} else {
						ret[i] = reflect.Zero(t.Out(i))
					}
				}
				if shouldPanic {
					pErr = pl.sV(err, true)
				}
			}
			pl.sync(func() {
				for _, sv := range rets[0 : len(rets)-1] {
					C.glue_dec(pl.thx, sv)
				}
				if err != nil {
					C.glue_dec(pl.thx, err)
				}
			})
			if pErr != nil {
				panic(pErr)
			}
			return
		}))
		cv = pl.sV(src, true)
		return
	case reflect.Interface:
		if t == reflect.TypeOf((*error)(nil)).Elem() {
			dst.Set(reflect.ValueOf(pl.sV(src, true)))
			return
		}
	case reflect.Map:
		dst.Set(reflect.MakeMap(t))
		cb := func(key *C.SV, val *C.SV) {
			pl.unsync(func() {
				k := reflect.New(t.Key()).Elem()
				pl.valSV(&k, key)
				v := reflect.New(t.Elem()).Elem()
				pl.valSV(&v, val)
				dst.SetMapIndex(k, v)
			})
		}
		ptr := C.IV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkHV(pl.thx, src, ptr) })
		return
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.sV wrapping
		if t == reflect.TypeOf((*sV)(nil)) {
			dst.Set(reflect.ValueOf(pl.sV(src, false)))
			return
		}
	case reflect.Slice:
		// TODO: this is sketchy, refactor
		tmp := reflect.MakeSlice(t, 0, 0)
		cb := func(sv *C.SV) {
			pl.unsync(func() {
				val := reflect.New(t.Elem()).Elem()
				pl.valSV(&val, sv)
				tmp = reflect.Append(tmp, val)
			})
		}
		ptr := C.IV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkAV(pl.thx, src, ptr) })
		dst.Set(tmp)
		return
	case reflect.String:
		var val *C.char
		var len C.STRLEN
		pl.sync(func() { val = C.glue_getPV(pl.thx, src, &len) })
		dst.SetString(C.GoStringN(val, C.int(len)))
		return
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + t.Kind().String() + "\"")
}

func svFini(sv *sV) {
	if sv.own {
		if false {
			fmt.Printf("RELEASE %p\n", sv.sv)
		}
		sv.pl.sync(func() { C.glue_dec(sv.pl.thx, sv.sv) })
	}
}

func (pl *PL) sV(sv *C.SV, own bool) *sV {
	var self sV
	self.pl = pl
	self.sv = sv
	self.own = own
	pl.sync(func() { C.glue_inc(pl.thx, sv) })
	runtime.SetFinalizer(&self, svFini)
	if self.own && false {
		pl.sync(func() { C.glue_track(pl.thx, sv) })
		fmt.Printf("ACQUIRE %p\n", self.sv)
	}
	return &self
}

func (sv *sV) Error() string {
	v := reflect.New(reflect.TypeOf((*string)(nil)).Elem()).Elem()
	sv.pl.valSV(&v, sv.sv)
	return v.String()
}

//export goInvoke
func goInvoke(data int, arg unsafe.Pointer, ret unsafe.Pointer) {
	liveCB[data](arg, ret)
}

//export goRelease
func goRelease(data int) {
	// if this gets complicated, remember to unlock/lock pl.mx
	delete(liveCB, data)
}
