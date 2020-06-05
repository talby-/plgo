// Package plgo provides a Perl runtime to Go
package plgo

//go:generate ./gen.pl $GOFILE

/*
#cgo CFLAGS: -Wall -D_REENTRANT -D_GNU_SOURCE -DDEBIAN -fstack-protector -fno-strict-aliasing -pipe -I/usr/local/include -D_LARGEFILE_SOURCE -D_FILE_OFFSET_BITS=64  -I/usr/lib/perl/5.18/CORE
#cgo LDFLAGS: -L/usr/local/lib  -L/usr/lib/perl/5.18/CORE -lperl -ldl -lm -lpthread -lc -lcrypt
#include "glue.h"
*/
import "C"
import (
	"fmt"
	"reflect"
	"runtime"
	"unsafe"
	"sync"
)

// PL holds a Perl runtime
type PL struct {
	thx        *C.PerlInterpreter
	cx         chan bool
	Preamble   string // prepended to any plgo.Eval() call
	newSVcmplx func(float64, float64) *sV
	valSVcmplx func(*sV) (float64, float64)
}

type sV struct {
	pl  *PL
	sv  *C.SV
	own bool
}

type errFunc func(error) bool

type liveSTEnt struct {
	live int
	getf func(*C.char) *C.SV
	setf func(*C.char, *C.SV)
	call func(*C.char, **C.SV) **C.SV
}

// We can not reliably hold pointers to Go objects in C
// https://github.com/golang/go/issues/12416 documents the rules.
// runtime.GC() can move objects in memory so we have to create an
// indirection layer.  The live maps will serve this purpose.
var (
	liveCBSeq = uint(0)
	liveCB    = map[uint]func(**C.SV) **C.SV{}
	liveSTSeq = uint(0)
	liveST    = map[uint]*liveSTEnt{}
	liveMX	  = &sync.RWMutex{}
)

func plFini(pl *PL) {
	<-pl.cx
	C.glue_fini(pl.thx)
	pl.cx <- true
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

func sliceOf(raw **C.SV, n int) []*C.SV {
	return *(*[]*C.SV)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(raw)),
		Len:  n,
		Cap:  n,
	}))
}

/* error handling though this code is a bit unconventional.  The API
 * style we're providing lets the caller decide if we should populate an
 * error object return value, or panic().  We have a helper function to
 * support that convention, but it's still an awkward constraint. */
var errNoop = fmt.Errorf("noop error")

func splitErrs(rets []reflect.Value) (outs []reflect.Value, ef errFunc) {
	et := reflect.TypeOf((*error)(nil)).Elem()
	var errs []reflect.Value
	outs = make([]reflect.Value, 0)
	for _, v := range rets {
		if v.Type() == et {
			errs = append(errs, v)
		} else {
			outs = append(outs, v)
		}
	}
	if len(errs) == 0 {
		ef = func(_ error) bool { return false }
	} else {
		ef = func(ifc error) bool {
			err := ifc.(error)
			// this is silly, the errNoop can be sent to this
			// function to detect if error handling is present or if the
			// caller should instead call panic.  This funciton can't
			// just call panic for the caller because that would report
			// the error as happening here. :(
			if err != errNoop {
				val := reflect.ValueOf(err)
				for _, v := range errs {
					v.Set(val)
				}
			}
			return true
		}
	}
	return
}

// Eval will execute a string of Perl code.  If ptrs are provided,
// the list of results from Perl will be stored in the list of ptrs.
// Not all types are supported, but many basic types are, including
// functions.
func (pl *PL) Eval(text string, ptrs ...interface{}) {
	var av *C.SV

	// convert ptrs to Values
	rets := make([]reflect.Value, len(ptrs))
	for i, p := range ptrs {
		ptr := reflect.ValueOf(p)
		if ptr.Kind() == reflect.Ptr {
			rets[i] = ptr.Elem()
			rets[i].Set(reflect.Zero(rets[i].Type()))
		} else {
			panic(fmt.Errorf("argument %d must be a pointer", 1+i))
		}
	}
	rets, errf := splitErrs(rets)

	// run eval()
	code := C.CString(pl.Preamble + "; [ do { \n#line 1 \"plgo.Eval()\"\n" + text + "\n } ]")
	var errsv *C.SV
	<-pl.cx
	av = C.glue_eval(pl.thx, code, &errsv)
	pl.cx <- true
	defer func() {
		<-pl.cx
		C.glue_dec(pl.thx, av)
		C.glue_dec(pl.thx, errsv)
		pl.cx <- true
	}()
	if errsv != nil {
		err := pl.sV(errsv, true)
		if errf(err) {
			return
		}
		panic(err)
	}

	if len(rets) > 0 {
		// copy out rets
		cb := func(raw **C.SV, n C.IV) {
			pl.cx <- true
			defer func() { <-pl.cx }()
			lst := sliceOf(raw, int(n))
			for i, v := range rets {
				pl.getSV(&v, lst[i], errf)
			}
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		<-pl.cx
		C.glue_walkAV(pl.thx, av, ptr)
		pl.cx <- true
	}
}

// Live counts the number of live variables in the Perl instance.
// This function is used for leak detection in the test code.
// runtime.GC() must be called to get accurate live value counts.
func (pl *PL) Live() int {
	var rv C.IV
	<-pl.cx
	rv = C.glue_count_live(pl.thx)
	pl.cx <- true
	return int(rv)
}

func (pl *PL) setSV(ptr **C.SV, src reflect.Value, errf errFunc) bool {
	t := src.Type()
	switch src.Kind() {
	case reflect.Bool:
		<-pl.cx
		C.glue_setBool(pl.thx, ptr, C.bool(src.Bool()))
		pl.cx <- true
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		<-pl.cx
		C.glue_setIV(pl.thx, ptr, C.IV(src.Int()))
		pl.cx <- true
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		<-pl.cx
		C.glue_setUV(pl.thx, ptr, C.UV(src.Uint()))
		pl.cx <- true
		return true
	case reflect.Float32, reflect.Float64:
		<-pl.cx
		C.glue_setNV(pl.thx, ptr, C.NV(src.Float()))
		pl.cx <- true
		return true
	case reflect.Complex64, reflect.Complex128:
		if pl.newSVcmplx == nil {
			pl.Eval(`
				require Math::Complex;
				sub {
					my $rv = Math::Complex->new(0, 0);
					$rv->_set_cartesian([ @_ ]);
					return $rv;
				}
			`, &pl.newSVcmplx)
		}
		v := src.Complex()
		sv := pl.newSVcmplx(real(v), imag(v))
		*ptr = sv.sv
		return true
	case reflect.Array,
		reflect.Slice:
		lst := make([]*C.SV, 1+src.Len())
		for i := range lst[0 : len(lst)-1] {
			if !pl.setSV(&lst[i], src.Index(i), errf) {
				return false
			}
		}
		<-pl.cx
		C.glue_setAV(pl.thx, ptr, &lst[0])
		pl.cx <- true
		return true
	case reflect.Chan:
	case reflect.Func:
		call := func(arg **C.SV) (ret **C.SV) {
			// TODO: need an error proxy
			pl.cx <- true
			defer func() { <-pl.cx }()
			// xlate args - they are already mortal, don't take
			// ownership unless they need to survive beyond the
			// function call
			args := make([]reflect.Value, t.NumIn())
			for i, sv := range sliceOf(arg, len(args)) {
				args[i] = reflect.New(t.In(i)).Elem()
				pl.getSV(&args[i], sv, errf)
			}
			// xlate rets - return as owning references and
			// glue_invoke() will mortalize them for us
			ret = C.glue_alloc(C.IV(1 + t.NumOut()))
			rets := sliceOf(ret, t.NumOut())
			for i, val := range src.Call(args) {
				pl.setSV(&rets[i], val, errf)
			}
			return
		}
		liveMX.Lock();
		liveCBSeq++
		id := liveCBSeq
		liveCB[liveCBSeq] = call
		liveMX.Unlock()
		<-pl.cx
		C.glue_setCV(pl.thx, ptr, C.UV(id))
		pl.cx <- true
		return true
	case reflect.Interface:
	case reflect.Map:
		keys := src.MapKeys()
		lst := make([]*C.SV, len(keys)<<1+1)
		for i, key := range keys {
			if !pl.setSV(&lst[i<<1], key, errf) {
				return false
			}
			if !pl.setSV(&lst[i<<1+1], src.MapIndex(key), errf) {
				return false
			}
		}
		<-pl.cx
		C.glue_setHV(pl.thx, ptr, &lst[0])
		pl.cx <- true
		return true
	case reflect.Ptr:
		// TODO: *sV handling is a special case, but generic Ptr support
		// could be implemented
		if t == reflect.TypeOf((*sV)(nil)) {
			*ptr = src.Interface().(*sV).sv
			return true
		}
	case reflect.String:
		str := src.String()
		<-pl.cx
		C.glue_setPV(pl.thx, ptr, C.CString(str), C.STRLEN(len(str)))
		pl.cx <- true
		return true
	case reflect.Struct:
		ent := new(liveSTEnt)
		liveMX.Lock()
		liveSTSeq++
		liveST[liveSTSeq] = ent
		id := liveSTSeq
		liveMX.Unlock()
		nm := C.CString(t.PkgPath() + "/" + t.Name())
		al := make([]*C.char, 1+t.NumField())
		ent.getf = func(name *C.char) (rv *C.SV) {
			// TODO: need an error proxy
			pl.cx <- true
			pl.setSV(&rv, src.FieldByName(C.GoString(name)), errf)
			<-pl.cx
			return
		}
		ent.setf = func(name *C.char, sv *C.SV) {
			// TODO: need an error proxy
			pl.cx <- true
			val := src.FieldByName(C.GoString(name))
			pl.getSV(&val, sv, errf)
			<-pl.cx
		}
		ent.call = func(name *C.char, arg **C.SV) (ret **C.SV) {
			// TODO: need an error proxy
			pl.cx <- true
			m := src.MethodByName(C.GoString(name))
			mt := m.Type()
			args := make([]reflect.Value, mt.NumIn())
			for i, sv := range sliceOf(arg, len(args)) {
				args[i] = reflect.New(mt.In(i)).Elem()
				pl.getSV(&args[i], sv, errf)
			}
			ret = C.glue_alloc(C.IV(1 + mt.NumOut()))
			rets := sliceOf(ret, 1+mt.NumOut())
			for i, val := range m.Call(args) {
				pl.setSV(&rets[i], val, errf)
			}
			<-pl.cx
			return
		}
		ent.live = len(al) /* held by the wrap + each field stub */
		for i := range al[0 : len(al)-1] {
			al[i] = C.CString(t.Field(i).Name)
		}
		<-pl.cx
		C.glue_setObj(pl.thx, ptr, C.UV(id), nm, &al[0])
		pl.cx <- true
		return true
	case reflect.UnsafePointer:
	}
	err := fmt.Errorf(`unhandled type "%s"`, src.Kind().String())
	if errf(err) {
		return false
	}
	panic(err)
}

func (pl *PL) getSV(dst *reflect.Value, src *C.SV, errf errFunc) bool {
	t := dst.Type()
	switch t.Kind() {
	case reflect.Bool:
		var val C.bool
		<-pl.cx
		C.glue_getBool(pl.thx, &val, src)
		pl.cx <- true
		dst.SetBool(bool(val))
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var val C.IV
		<-pl.cx
		C.glue_getIV(pl.thx, &val, src)
		pl.cx <- true
		dst.SetInt(int64(val))
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		var val C.UV
		<-pl.cx
		C.glue_getUV(pl.thx, &val, src)
		pl.cx <- true
		dst.SetUint(uint64(val))
		return true
	case reflect.Float32, reflect.Float64:
		var val C.NV
		<-pl.cx
		C.glue_getNV(pl.thx, &val, src)
		pl.cx <- true
		dst.SetFloat(float64(val))
		return true
	case reflect.Complex64, reflect.Complex128:
		if pl.valSVcmplx == nil {
			pl.Eval(`
				require Math::Complex;
				sub {
					return Math::Complex::Re($_[0]), Math::Complex::Im($_[0]);
				}
			`, &pl.valSVcmplx)
		}
		// TODO: check if errf to decide if callee should panic
		re, im := pl.valSVcmplx(pl.sV(src, false))
		dst.SetComplex(complex128(complex(re, im)))
		return true
	case reflect.Array:
	case reflect.Chan:
	case reflect.Func:
		cv := pl.sV(src, true)
		dst.Set(reflect.MakeFunc(t, func(arg []reflect.Value) (outs []reflect.Value) {
			// This ends up looking a lot like Eval(), but we have input
			// args to convert and an SV instead of a string to execute.

			// first scan outputs, so we can get error handling correct
			// asap.
			outs = make([]reflect.Value, t.NumOut())
			for i := range outs {
				outs[i] = reflect.New(t.Out(i)).Elem()
			}
			ret, errh := splitErrs(outs)

			args := make([]*C.SV, 1+t.NumIn())
			for i, val := range arg {
				if !pl.setSV(&args[i], val, errh) {
					return
				}
			}

			rets := make([]*C.SV, 1+len(ret))

			// make the call
			no := C.IV(len(ret))
			<-pl.cx
			esv := C.glue_call_sv(pl.thx, cv.sv, &args[0], &rets[0], no)
			pl.cx <- true
			defer func() {
				<-pl.cx
				for _, sv := range rets {
					C.glue_dec(pl.thx, sv)
				}
				C.glue_dec(pl.thx, esv)
				pl.cx <- true
			}()
			if esv != nil {
				err := pl.sV(esv, true)
				if errh(err) {
					return
				}
				panic(err)
			}

			for i, v := range ret {
				// try converting rvs
				if !pl.getSV(&v, rets[i], errh) {
					return
				}
			}
			return
		}))
		return true
	case reflect.Interface:
		if t == reflect.TypeOf((*error)(nil)).Elem() {
			dst.Set(reflect.ValueOf(pl.sV(src, true)))
			return true
		}
	case reflect.Map:
		cb := func(raw **C.SV, iv C.IV) {
			pl.cx <- true
			defer func() { <-pl.cx }()
			n := int(iv)
			if n >= 0 {
				dst.Set(reflect.MakeMap(t))
				var k reflect.Value
				for i, sv := range sliceOf(raw, n) {
					switch i & 1 {
					case 0:
						k = reflect.New(t.Key()).Elem()
						if !pl.getSV(&k, sv, errf) {
							return
						}
					case 1:
						v := reflect.New(t.Elem()).Elem()
						if !pl.getSV(&v, sv, errf) {
							return
						}
						dst.SetMapIndex(k, v)
					}
				}
			} else {
				err := fmt.Errorf("unable to convert SV to Map")
				if errf(err) {
					return
				}
				panic(err)
			}
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		<-pl.cx
		C.glue_walkHV(pl.thx, src, ptr)
		pl.cx <- true
		return true
	case reflect.Ptr:
		// TODO: for now we're only handling *plgo.sV wrapping
		if t == reflect.TypeOf((*sV)(nil)) {
			dst.Set(reflect.ValueOf(pl.sV(src, false)))
			return true
		}
	case reflect.Slice:
		var err error
		errh := func(ev error) bool {
			err = ev
			return true
		}
		cb := func(raw **C.SV, iv C.IV) {
			pl.cx <- true
			defer func() { <-pl.cx }()
			n := int(iv)
			if n >= 0 {
				dst.Set(reflect.MakeSlice(t, n, n))
				for i, sv := range sliceOf(raw, n) {
					val := dst.Index(i)
					if !pl.getSV(&val, sv, errh) {
						return
					}
				}
			} else {
				errh(fmt.Errorf("unable to convert SV to Slice"))
				return
			}
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		<-pl.cx
		C.glue_walkAV(pl.thx, src, ptr)
		pl.cx <- true
		if err != nil {
			if errf(err) {
				return false
			}
			panic(err)
		}
		return true
	case reflect.String:
		var str *C.char
		var len C.STRLEN
		<-pl.cx
		C.glue_getPV(pl.thx, &str, &len, src)
		pl.cx <- true
		dst.SetString(C.GoStringN(str, C.int(len)))
		return true
	case reflect.Struct:
		/* TODO: this should also unwrap proxies */
		var err error
		errh := func(ev error) bool {
			err = ev
			return true
		}
		cb := func(raw **C.SV, n C.IV) {
			pl.cx <- true
			defer func() { <-pl.cx }()
			dst.Set(reflect.New(t).Elem())
			k := reflect.New(reflect.TypeOf((*string)(nil)).Elem()).Elem()
			for i, sv := range sliceOf(raw, int(n)) {
				switch i & 1 {
				case 0:
					if !pl.getSV(&k, sv, errh) {
						return
					}
				case 1:
					v := dst.FieldByName(k.String())
					if v.IsValid() {
						if !pl.getSV(&v, sv, errh) {
							return
						}
					}
				}
			}
		}
		ptr := C.UV(uintptr(unsafe.Pointer(&cb)))
		<-pl.cx
		C.glue_walkHV(pl.thx, src, ptr)
		pl.cx <- true
		if err != nil {
			if errf(err) {
				return false
			}
			panic(err)
		}
		return true
	case reflect.UnsafePointer:
	}
	err := fmt.Errorf(`unhandled type "%v"`, t.Kind().String())
	if errf(err) {
		return false
	}
	panic(err)
}

func svFini(sv *sV) {
	if sv.own {
		<-sv.pl.cx
		C.glue_dec(sv.pl.thx, sv.sv)
		sv.pl.cx <- true
	}
}

func (pl *PL) sV(sv *C.SV, own bool) *sV {
	var self sV
	self.pl = pl
	self.sv = sv
	self.own = own
	<-pl.cx
	C.glue_inc(pl.thx, sv)
	pl.cx <- true
	runtime.SetFinalizer(&self, svFini)
	return &self
}

func (sv *sV) Error() string {
	v := reflect.New(reflect.TypeOf((*string)(nil)).Elem()).Elem()
	sv.pl.getSV(&v, sv.sv, func(err error) bool {
		// TODO: getSV can return an error, handle it *somehow*
		return false
	})
	return v.String()
}

//export goList
func goList(cb uintptr, lst **C.SV, n C.IV) {
	(*(*func(**C.SV, C.IV))(unsafe.Pointer(cb)))(lst, n)
}

//export goInvoke
func goInvoke(data uint, arg **C.SV) **C.SV {
	liveMX.RLock()
	call := liveCB[data]
	liveMX.RUnlock()
	return call(arg)
}

//export goReleaseCB
func goReleaseCB(data uint) {
	liveMX.Lock()
	delete(liveCB, data)
	liveMX.Unlock()
}

//export goSTGetf
func goSTGetf(id uint, name *C.char) *C.SV {
	liveMX.RLock()
	ent := liveST[id]
	liveMX.RUnlock()
	return ent.getf(name)
}

//export goSTSetf
func goSTSetf(id uint, name *C.char, sv *C.SV) {
	liveMX.RLock()
	ent := liveST[id]
	liveMX.RUnlock()
	ent.setf(name, sv)
}

//export goSTCall
func goSTCall(id uint, name *C.char, arg **C.SV) **C.SV {
	liveMX.RLock()
	ent := liveST[id]
	liveMX.RUnlock()
	return ent.call(name, arg)
}

//export goReleaseST
func goReleaseST(id uint) {
	liveMX.Lock();
	liveST[id].live--
	if liveST[id].live <= 0 {
		delete(liveST, id)
	}
	liveMX.Unlock();
}
