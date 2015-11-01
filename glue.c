
#include "glue.h"
#include "XSUB.h"
#include "_cgo_export.h"

/* notes: pretty much any char* coming from Go is going to callee
 * allocated and caller freed so that the go side isn't peppered with
 * tons of defer calls */

extern void boot_DynaLoader (pTHX_ CV *cv);

static void xs_init(pTHX) {
    newXS("DynaLoader::boot_DynaLoader", boot_DynaLoader, __FILE__);
}

const char *init_text =
    "package PlGo;"\
    "use strict;"\
    "use warnings;"\
    "1;"\
;


PerlInterpreter *glue_init() {
    int argc = 3;
    char *argv[] = { "", "-e", "0", NULL };
    
    PerlInterpreter *my_perl;
    my_perl = perl_alloc();
    perl_construct(my_perl);
    perl_parse(my_perl, xs_init, argc, argv, NULL);
    PL_exit_flags |= PERL_EXIT_DESTRUCT_END;
    return my_perl;
}

void glue_fini(pTHX) {
    perl_destruct(my_perl);
    perl_free(my_perl);
}

SV *glue_eval(pTHX_ char *text, SV **errp) {
    SV *rv = eval_pv(text, TRUE);
    *errp = SvTRUE(ERRSV) ?  newSVsv(ERRSV) : NULL;
    free(text);
    return rv;
}

SV *glue_call_sv(pTHX_ SV *sv, SV **args, SV **errp) {
    I32 ax;
    int i;
    SV *rv;
    dSP;

    ENTER;
    SAVETMPS;
    PUSHMARK(SP);
    while(*args)
        XPUSHs(*args++);
    PUTBACK;
    i = call_sv(sv, G_EVAL | G_SCALAR);
    SPAGAIN;
    if(SvTRUE(ERRSV)) {
        *errp = newSVsv(ERRSV);
    } else {
        *errp = NULL;
        rv = POPs;
        SvREFCNT_inc(rv);
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    if(*errp)
        return NULL;
    return rv;
}

SV *glue_call_method(pTHX_
    char *method,
    SV **args,
    SV **errp
) {
    I32 ax;
    int i;
    SV *rv;
    dSP;

    ENTER;
    SAVETMPS;
    PUSHMARK(SP);
    while(*args)
        XPUSHs(*args++);
    PUTBACK;
    i = call_method(method, G_EVAL | G_SCALAR);
    SPAGAIN;
    if(SvTRUE(ERRSV)) {
        *errp = newSVsv(ERRSV);
    } else {
        *errp = NULL;
        rv = POPs;
        SvREFCNT_inc(rv);
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    free(method);
    if(*errp)
        return NULL;
    return rv;
}

void glue_inc(pTHX_ SV *sv) { SvREFCNT_inc(sv); }

void glue_dec(pTHX_ SV *sv) { SvREFCNT_dec(sv); }

SV *glue_undef(pTHX) { return &PL_sv_undef; }

bool glue_getBool(pTHX_ SV *sv) { return SvTRUE(sv); }

IV glue_getIV(pTHX_ SV *sv) { return SvIV(sv); }

UV glue_getUV(pTHX_ SV *sv) { return SvUV(sv); }

NV glue_getNV(pTHX_ SV *sv) { return SvNV(sv); }

const char *glue_getPV(pTHX_ SV *sv, STRLEN *len) { return SvPV(sv, *len); }

SV *glue_newBool(pTHX_ bool v) { return boolSV(v); }

SV *glue_newIV(pTHX_ IV v) { return newSViv(v); }

SV *glue_newUV(pTHX_ UV v) { return newSVuv(v); }

SV *glue_newNV(pTHX_ NV v) { return newSVnv(v); }

SV *glue_newPV(pTHX_ char *str, STRLEN len) {
    SV *rv = newSVpvn(str, len);
    free(str);
    return rv;
}

SV *glue_newAV(pTHX_ SV **elts) {
    AV *av = newAV();
    while(*elts)
        av_push(av, *elts++);
    return newRV_inc((SV *)av);
}

SV *glue_newHV(pTHX_ SV **elts) {
    HV *hv = newHV();
    while(*elts) {
        SV *k = *elts++;
        SV *v = *elts++;
        hv_store_ent(hv, k, v, 0);
    }
    return newRV_inc((SV *)hv);
}

static MGVTBL glue_vtbl = { 0, 0, 0, 0, 0, 0, 0, 0 };

typedef struct {
    GoInterface *call;
    GoInterface *data;
} glue_cb_t;

XS(glue_invoke)
{
    dXSARGS;
    MAGIC *mg;
   
    mg = mg_findext((SV *)cv, PERL_MAGIC_ext, &glue_vtbl);
    glue_cb_t *cb = (glue_cb_t *)mg->mg_ptr;
    SV **args = alloca(items * sizeof(SV *));
    int i;
    for(i = 0; i < items; i++)
        args[i] = ST(i);
    SV **rvs = go_invoke(cb->call, cb->data, items, args);
    for(i = 0; *rvs; i++)
        ST(i) = *rvs++;
    XSRETURN(i);
}

SV *glue_newCV(pTHX_ void *call, void *data) {
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    glue_cb_t cb = { call, data };
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &glue_vtbl, (char *)&cb, sizeof(cb));
    return newRV_noinc((SV *)cv);
}
