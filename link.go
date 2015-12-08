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
	"reflect"
	"runtime"
	"unsafe"
)

// PL holds a Perl runtime
type PL struct {
	thx        C.gPL
	cx         chan bool
	newSVcmplx func(float64, float64) *sV
	valSVcmplx func(*sV) (float64, float64)
}

type sV struct {
	pl  *PL
	sv  C.gSV
	own bool
}

type liveSTEnt struct {
	live int
	getf func(*C.char) C.gSV
	setf func(*C.char, C.gSV)
	call func(*C.char, *C.gSV) *C.gSV
}

// We can not reliably hold pointers to Go objects in C
// https://github.com/golang/go/issues/12416 documents the rules.
// runtime.GC() can move objects in memory so we have to create an
// indirection layer.  The live maps will serve this purpose.
var (
	liveCBSeq = uint(0)
	liveCB    = map[uint]func(*C.gSV) *C.gSV{}
	liveSTSeq = uint(0)
	liveST    = map[uint]*liveSTEnt{}
)

func plFini(pl *PL) {
	pl.sync(func() { C.glue_fini(pl.thx) })
}

// New initializes a Perl runtime
func New() *PL {
	pl := new(PL)
	pl.thx = C.glue_init()
	pl.cx = make(chan bool, 1)
	runtime.SetFinalizer(pl, plFini)
	pl.cx <- true // this PL is now open for business
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

func sliceOf(raw *C.gSV, n int) []C.gSV {
	return *(*[]C.gSV)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(raw)),
		Len:  n,
		Cap:  n,
	}))
}

// Eval will execute a string of Perl code.  If ptrs are provided,
// the list of results from Perl will be stored in the list of ptrs.
// Not all types are supported, but many basic types are, including
// functions.
func (pl *PL) Eval(text string, ptrs ...interface{}) error {
	var err, av C.gSV
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
		cb := func(raw *C.gSV, n C.IV) {
			pl.unsync(func() {
				lst := sliceOf(raw, int(n))
				for i, sv := range lst {
					val := reflect.ValueOf(ptrs[i]).Elem()
					pl.valSV(&val, sv)
				}
			})
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
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

func (pl *PL) newSVval(src reflect.Value) (dst C.gSV) {
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
		lst := make([]C.gSV, 1+src.Len())
		for i := range lst[0 : len(lst)-1] {
			lst[i] = pl.newSVval(src.Index(i))
		}
		pl.sync(func() { dst = C.glue_newAV(pl.thx, &lst[0]) })
		return
	case reflect.Chan:
	case reflect.Func:
		liveCBSeq++
		id := C.UV(liveCBSeq)
		liveCB[liveCBSeq] = func(arg *C.gSV) (ret *C.gSV) {
			pl.unsync(func() {
				// xlate args - they are already mortal, don't take
				// ownership unless they need to survive beyond the
				// function call
				args := make([]reflect.Value, t.NumIn())
				for i, sv := range sliceOf(arg, len(args)) {
					args[i] = reflect.New(t.In(i)).Elem()
					pl.valSV(&args[i], sv)
				}
				// xlate rets - return as owning references and
				// glue_invoke() will mortalize them for us
				ret = C.glue_alloc(C.IV(1 + t.NumOut()))
				rets := sliceOf(ret, t.NumOut())
				for i, val := range src.Call(args) {
					rets[i] = pl.newSVval(val)
				}
			})
			return
		}
		pl.sync(func() { dst = C.glue_newCV(pl.thx, id) })
		return
	case reflect.Interface:
	case reflect.Map:
		keys := src.MapKeys()
		lst := make([]C.gSV, len(keys)<<1+1)
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
		ent := new(liveSTEnt)
		liveSTSeq++
		liveST[liveSTSeq] = ent
		id := C.UV(liveSTSeq)
		ty := C.CString(t.PkgPath() + "/" + t.Name())
		al := make([]*C.char, 1+t.NumField())
		ent.getf = func(name *C.char) (rv C.gSV) {
			pl.unsync(func() {
				rv = pl.newSVval(src.FieldByName(C.GoString(name)))
			})
			return
		}
		ent.setf = func(name *C.char, sv C.gSV) {
			pl.unsync(func() {
				val := src.FieldByName(C.GoString(name))
				pl.valSV(&val, sv)
			})
		}
		ent.call = func(name *C.char, arg *C.gSV) (ret *C.gSV) {
			pl.unsync(func() {
				m := src.MethodByName(C.GoString(name))
				mt := m.Type()
				args := make([]reflect.Value, mt.NumIn())
				for i, sv := range sliceOf(arg, len(args)) {
					args[i] = reflect.New(mt.In(i)).Elem()
					pl.valSV(&args[i], sv)
				}
				ret = C.glue_alloc(C.IV(1 + mt.NumOut()))
				rets := sliceOf(ret, 1+mt.NumOut())
				for i, val := range m.Call(args) {
					rets[i] = pl.newSVval(val)
				}
			})
			return
		}
		ent.live = len(al) /* held by the wrap + each field stub */
		for i := range al[0 : len(al)-1] {
			al[i] = C.CString(t.Field(i).Name)
		}
		pl.sync(func() { dst = C.glue_newObj(pl.thx, id, ty, &al[0]) })
		return
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + src.Kind().String() + "\"")
}

func (pl *PL) valSV(dst *reflect.Value, src C.gSV) {
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
			args := make([]C.gSV, 1+t.NumIn())
			for i, val := range arg {
				args[i] = pl.newSVval(val)
			}
			rets := make([]C.gSV, 1+t.NumOut())

			var err C.gSV
			no := C.IV(t.NumOut())
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
		cb := func(raw *C.gSV, n int) {
			pl.unsync(func() {
				dst.Set(reflect.MakeMap(t))
				if raw == nil {
					return
				}
				var k reflect.Value
				for i, sv := range sliceOf(raw, int(n)) {
					if i&1 == 0 {
						k = reflect.New(t.Key()).Elem()
						pl.valSV(&k, sv)
					} else {
						v := reflect.New(t.Elem()).Elem()
						pl.valSV(&v, sv)
						dst.SetMapIndex(k, v)
					}
				}
			})
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkHV(pl.thx, src, ptr) })
		return
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.sV wrapping
		if t == reflect.TypeOf((*sV)(nil)) {
			dst.Set(reflect.ValueOf(pl.sV(src, false)))
			return
		}
	case reflect.Slice:
		cb := func(raw *C.gSV, n C.IV) {
			pl.unsync(func() {
				dst.Set(reflect.MakeSlice(t, int(n), int(n)))
				if raw == nil {
					return
				}
				for i, sv := range sliceOf(raw, int(n)) {
					val := dst.Index(i)
					pl.valSV(&val, sv)
				}
			})
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkAV(pl.thx, src, ptr) })
		return
	case reflect.String:
		var val *C.char
		var len C.STRLEN
		pl.sync(func() { val = C.glue_getPV(pl.thx, src, &len) })
		dst.SetString(C.GoStringN(val, C.int(len)))
		return
	case reflect.Struct:
		/* TODO: this should also unwrap proxies */
		cb := func(raw *C.gSV, n C.IV) {
			pl.unsync(func() {
				dst.Set(reflect.New(t).Elem())
				if raw == nil {
					return
				}
				k := reflect.New(reflect.TypeOf((*string)(nil)).Elem()).Elem()
				for i, sv := range sliceOf(raw, int(n)) {
					if i&1 == 0 {
						pl.valSV(&k, sv)
					} else {
						v := dst.FieldByName(k.String())
						if v.IsValid() {
							pl.valSV(&v, sv)
						}
					}
				}
			})
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		pl.sync(func() { C.glue_walkHV(pl.thx, src, ptr) })
		return

	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + t.Kind().String() + "\"")
}

func svFini(sv *sV) {
	if sv.own {
		sv.pl.sync(func() { C.glue_dec(sv.pl.thx, sv.sv) })
	}
}

func (pl *PL) sV(sv C.gSV, own bool) *sV {
	var self sV
	self.pl = pl
	self.sv = sv
	self.own = own
	pl.sync(func() { C.glue_inc(pl.thx, sv) })
	runtime.SetFinalizer(&self, svFini)
	return &self
}

func (sv *sV) Error() string {
	v := reflect.New(reflect.TypeOf((*string)(nil)).Elem()).Elem()
	sv.pl.valSV(&v, sv.sv)
	return v.String()
}

//export goList
func goList(cb uintptr, lst *C.gSV, n C.IV) {
	(*(*func(*C.gSV, C.IV))(unsafe.Pointer(cb)))(lst, n)
}

//export goInvoke
func goInvoke(data uint, arg *C.gSV) *C.gSV {
	return liveCB[data](arg)
}

//export goReleaseCB
func goReleaseCB(data uint) {
	delete(liveCB, data)
}

//export goSTGetf
func goSTGetf(id uint, name *C.char) C.gSV {
	return liveST[id].getf(name)
}

//export goSTSetf
func goSTSetf(id uint, name *C.char, sv C.gSV) {
	liveST[id].setf(name, sv)
}

//export goSTCall
func goSTCall(id uint, name *C.char, arg *C.gSV) *C.gSV {
	return liveST[id].call(name, arg)
}

//export goReleaseST
func goReleaseST(id uint) {
	liveST[id].live--
	if liveST[id].live > 0 {
		return
	}
	delete(liveST, id)
}
