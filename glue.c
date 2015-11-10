
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
    SV *rv;
    ENTER;
    SAVETMPS;
    rv = eval_pv(text, FALSE);
    if(SvTRUE(ERRSV)) {
        croak(SvPV_nolen(ERRSV));
        *errp = newSVsv(ERRSV);
    } else {
        *errp = NULL;
    }
    SvREFCNT_inc(rv);
    FREETMPS;
    LEAVE;
    free(text);
    return rv;
}

SV *glue_call_sv(pTHX_ SV *sv, SV **in, SV **out, int n_out) {
    I32 ax;
    int count;
    SV *err;
    dSP;
    int flags;

    switch(n_out) {
      case 0: flags |= G_VOID; break;
      case 1: flags |= G_SCALAR; break;
      default: flags |= G_ARRAY; break;
    }

    ENTER;
    SAVETMPS;
    PUSHMARK(SP);
    while(*in)
        mXPUSHs(*in++);
    PUTBACK;
    count = call_sv(sv, G_EVAL | flags);
    SPAGAIN;
    SP -= count;
    ax = (SP - PL_stack_base) + 1;
    if(SvTRUE(ERRSV)) {
        err = newSVsv(ERRSV);
    } else {
        int i;
        for(i = 0; i < count && i < n_out; i++) {
            out[i] = ST(i);
            SvREFCNT_inc(out[i]);
        }
        err = NULL;
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    return err;
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
        mXPUSHs(*args++);
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

void glue_sv_dump(pTHX_ SV *sv) { sv_dump(sv); }

bool glue_SvOK(pTHX_ SV *sv) { return SvOK(sv); }


int glue_count_live(pTHX) {
    /* Devel::Leak proved to be too expensive to run during scans, so
     * this lifts a bit of it's algorithm for something to give us
     * simple live variable allocation counts */
    SV *sva;
    int i, n;
    int rv = 0;
    for(sva = PL_sv_arenaroot; sva; sva = (SV *)SvANY(sva))
        for(i = 1, n = SvREFCNT(sva); i < n; i++)
            if(SvTYPE(sva + i) != SVTYPEMASK)
                rv++;
    return rv;
}

bool glue_getBool(pTHX_ SV *sv) { return SvTRUE(sv); }

IV glue_getIV(pTHX_ SV *sv) { return SvIV(sv); }

UV glue_getUV(pTHX_ SV *sv) { return SvUV(sv); }

NV glue_getNV(pTHX_ SV *sv) { return SvNV(sv); }

const char *glue_getPV(pTHX_ SV *sv, STRLEN *len) { return SvPV(sv, *len); }

bool glue_walkAV(pTHX_ SV *sv, void *data) {
    if(SvROK(sv)) {
        AV *av = (AV *)SvRV(sv);
        if(SvTYPE((SV *)av) == SVt_PVAV) {
            I32 i = 0;
            SAVETMPS;
            SV **eltp;
            while(eltp = av_fetch(av, i++, 0))
                glue_stepAV(data, *eltp);
            FREETMPS;
            return TRUE;
        }
    }
    return FALSE;
}

bool glue_walkHV(pTHX_ SV *sv, void *data) {
    if(SvROK(sv)) {
        HV *hv = (HV *)SvRV(sv);
        if(SvTYPE((SV *)hv) == SVt_PVHV) {
            HE *he;
            SV *key, *val;
            SAVETMPS;
            hv_iterinit(hv);
            while(he = hv_iternext(hv))
                glue_stepHV(data, HeSVKEY_force(he), HeVAL(he));
            FREETMPS;
            return TRUE;
        }
    }
    return FALSE;
}

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
    return newRV_noinc((SV *)av);
}

SV *glue_newHV(pTHX_ SV **elts) {
    HV *hv = newHV();
    SV *rv;
    while(*elts) {
        SV *k = *elts++;
        SV *v = *elts++;
        hv_store_ent(hv, k, v, 0);
        SvREFCNT_dec(k);
        // hv_store_ent has taken ownership of v
    }
    return newRV_noinc((SV *)hv);
}

SV *glue_newRV(pTHX_ SV *sv) {
    return newRV_inc(sv);
}


/* this struct holds the callback info */
typedef struct {
    void *call;
    int num_in;
    int num_out;
} glue_cb_t;

/* this function communicates up to Go when our handle on a callback is
 * no longer referenced */
static int glue_vtbl_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    glue_cb_t *cb = (glue_cb_t *)mg->mg_ptr;
    go_release(cb->call);
    return 0;
}

/* this table is used in the sv_magicext() call to tie a custom callback
 * to deallocation of a given SV */
static MGVTBL glue_vtbl = { 0, 0, 0, 0, glue_vtbl_sv_free };

/* this is the target from perl of a callback that should be routed to
 * Go */
XS(glue_invoke)
{
    dXSARGS;
    MAGIC *mg;
    SV **args, **rvs;
    int i;

    mg = mg_findext((SV *)cv, PERL_MAGIC_ext, &glue_vtbl);
    glue_cb_t *cb = (glue_cb_t *)mg->mg_ptr;
    if (items != cb->num_in)
        croak("expected %d args", cb->num_in);
    args = alloca(cb->num_in * sizeof(SV *));
    rvs = alloca(cb->num_out * sizeof(SV *));
    for(i = 0; i < cb->num_in; i++)
        args[i] = ST(i);
    go_invoke(cb->call, args, rvs);
    for(i = 0; i < cb->num_out; i++)
        ST(i) = rvs[i];
    XSRETURN(i);
}

/* the CV generated here will call into glue_invoke() */
SV *glue_newCV(pTHX_ void *call, IV num_in, IV num_out) {
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    glue_cb_t cb = { call, num_in, num_out };
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &glue_vtbl, (char *)&cb, sizeof(cb));
    return newRV_noinc((SV *)cv);
}
