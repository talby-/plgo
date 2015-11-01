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
}

type sv_t struct {
	pl *pl_t
	sv *C.SV
}

func New() pl_t {
	var self pl_t
	self.thx = C.glue_init()
	runtime.SetFinalizer(&self, func(dest *pl_t) {
		C.glue_fini(dest.thx)
	})
	return self
}

func (pl pl_t) svOfValue(src reflect.Value) *C.SV {
	switch src.Kind() {
	case reflect.Bool:
		return pl.svOf(src.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return pl.svOf(src.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return pl.svOf(src.Uint())
	case reflect.Uintptr:
	case reflect.Float32, reflect.Float64:
		return pl.svOf(src.Float())
	case reflect.Complex64:
	case reflect.Complex128:
	case reflect.Array,
		reflect.Slice:
		dst := make([]*C.SV, 1+src.Len())
		for i := 0; i < src.Len(); i++ {
			dst[i] = pl.svOf(src.Index(i).Interface())
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
	case reflect.String:
		return pl.svOf(src.String())
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unhandled type \"" + src.Kind().String() + "\"")
	return nil
}

func (pl pl_t) svOf(val interface{}) *C.SV {
	// handle some basic types and use reflect to handle the rest.
	// I'm assuming that the type switch is a bit faster than the
	// reflection interface where it's an option.
	switch src := val.(type) {
	case bool:
		return C.glue_newBool(pl.thx, C.bool(src))
	case int:
		return C.glue_newIV(pl.thx, C.IV(src))
	case int8:
		return C.glue_newIV(pl.thx, C.IV(src))
	case int16:
		return C.glue_newIV(pl.thx, C.IV(src))
	case int32:
		return C.glue_newIV(pl.thx, C.IV(src))
	case int64:
		return C.glue_newIV(pl.thx, C.IV(src))
	case uint:
		return C.glue_newUV(pl.thx, C.UV(src))
	case uint8:
		return C.glue_newUV(pl.thx, C.UV(src))
	case uint16:
		return C.glue_newUV(pl.thx, C.UV(src))
	case uint32:
		return C.glue_newUV(pl.thx, C.UV(src))
	case uint64:
		return C.glue_newUV(pl.thx, C.UV(src))
	case float32:
		return C.glue_newNV(pl.thx, C.NV(src))
	case float64:
		return C.glue_newNV(pl.thx, C.NV(src))
	case string:
		return C.glue_newPV(pl.thx, C.CString(src), C.STRLEN(len(src)))
	default:
		return pl.svOfValue(reflect.ValueOf(src))
	}
}

// the public SV constructor retain which THX it came from
func (pl pl_t) SV(val interface{}) *sv_t {
	var sv *C.SV
	switch val.(type) {
	case *C.SV:
		sv = val.(*C.SV)
		if sv == nil {
			return nil
		}
		C.glue_inc(pl.thx, sv)
	default:
		sv = pl.svOf(val)
	}
	self := new(sv_t)
	self.pl = &pl
	self.sv = sv
	runtime.SetFinalizer(self, func(dest *sv_t) {
		C.glue_dec(dest.pl.thx, dest.sv)
	})
	return self
}

func (pl pl_t) eval(text string, args []interface{}) (rv *sv_t, err *sv_t) {
	/* to get this to a point it will set up @_ we need to do the exec
	 * in two steps eval("sub { ... }")->(...)
	 * this is somewhat nice because this will let us separate "compile"
	 * and "run" time errors. */
	var e_sv *C.SV
	lst := make([]*C.SV, 1+len(args))
	for i, arg := range args {
		lst[i] = pl.svOf(arg)
	}
	cv := C.glue_eval(pl.thx, C.CString("sub {"+text+"}"), &e_sv)
	if e_sv == nil {
		rv = pl.SV(C.glue_call_sv(pl.thx, cv, &lst[0], &e_sv))
	}
	err = pl.SV(e_sv)
	return
}

func (pl pl_t) Eval(text string, args ...interface{}) (*sv_t, *sv_t) {
	return pl.eval(text, args)
}

func (pl pl_t) MustEval(text string, args ...interface{}) (rv *sv_t) {
	rv, err := pl.eval(text, args)
	if err != nil {
		panic(err)
	}
	return
}

// an sv_t should have an interface as similar to a reflect.Value as is
// practical to mimic...
//func (src *sv_t) Addr() *sv_t
//func (src *sv_t) Bool() bool
//func (src *sv_t) Bytes() []byte
//func (src *sv_t) Call(in []Value) []Value
//func (src *sv_t) CallSlice(in []Value) []Value
//func (src *sv_t) CanAddr() bool
//func (src *sv_t) CanInterface() bool
//func (src *sv_t) CanSet() bool
//func (src *sv_t) Cap() int
//func (src *sv_t) Close()
//func (src *sv_t) Complex() complex128
//func (src *sv_t) Convert(t Type) *sv_t


func (src *sv_t) Bool() bool {
	return bool(C.glue_getBool(src.pl.thx, src.sv))
}

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

func (s *sv_t) call(name string, args []interface{}) (rv *sv_t, err *sv_t) {
	var e_sv *C.SV
	lst := make([]*C.SV, 2+len(args))
	lst[0] = s.sv
	for i, arg := range args {
		lst[i+1] = s.pl.svOf(arg)
	}
	rv = s.pl.SV(C.glue_call_method(s.pl.thx, C.CString(name), &lst[0], &e_sv))
	err = s.pl.SV(e_sv)
	return
}

func (s *sv_t) Call(name string, args ...interface{}) (*sv_t, *sv_t) {
	return s.call(name, args)
}

func (s *sv_t) MustCall(name string, args ...interface{}) (rv *sv_t) {
	rv, err := s.call(name, args)
	if err != nil {
		panic(err)
	}
	return
}

func (src *sv_t) asValue(ty reflect.Type) reflect.Value {
	switch ty.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(bool(src.Bool()))
	case reflect.Int:
		return reflect.ValueOf(int(src.Int()))
	case reflect.Int8:
		return reflect.ValueOf(int8(src.Int()))
	case reflect.Int16:
		return reflect.ValueOf(int16(src.Int()))
	case reflect.Int32:
		return reflect.ValueOf(int32(src.Int()))
	case reflect.Int64:
		return reflect.ValueOf(int64(src.Int()))
	case reflect.Uint:
		return reflect.ValueOf(uint(src.Uint()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(src.Uint()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(src.Uint()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(src.Uint()))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(src.Uint()))
	case reflect.Uintptr:
	case reflect.Float32:
		return reflect.ValueOf(float32(src.Float()))
	case reflect.Float64:
		return reflect.ValueOf(float64(src.Float()))
	case reflect.Complex64:
	case reflect.Complex128:
	case reflect.Array:
	case reflect.Slice:
	case reflect.Chan:
	case reflect.Func:
	case reflect.Interface:
	case reflect.Map:
	case reflect.Ptr:
	case reflect.String:
		return reflect.ValueOf(string(src.String()))
	case reflect.Struct:
	case reflect.UnsafePointer:
	}
	panic("unable to convert SV to " + ty.Kind().String() + " Value")
	return reflect.ValueOf(nil)
}

//export go_invoke
func go_invoke(call *interface{}, data *interface{}, n int, args_raw unsafe.Pointer) **C.SV {
	// recover the thx wrap
	pl := (*data).(pl_t)

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
		args[i] = pl.SV(sv).asValue(ty.In(i))
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
