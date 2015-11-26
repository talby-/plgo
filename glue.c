
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

tTHX glue_init() {
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
    PERL_SET_CONTEXT(my_perl);
    perl_destruct(my_perl);
    perl_free(my_perl);
}

SV *glue_eval(pTHX_ char *text, SV **errp) {
    PERL_SET_CONTEXT(my_perl);
    SV *rv;
    ENTER;
    SAVETMPS;
    rv = eval_pv(text, FALSE);
    if(SvTRUE(ERRSV)) {
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

SV *glue_call_sv(pTHX_ SV *sv, SV **arg, SV **ret, int n) {
    I32 ax;
    int count;
    int flags;
    SV *err;
    dSP;

    PERL_SET_CONTEXT(my_perl);
    if(!SvROK(sv) || SvTYPE(SvRV(sv)) != SVt_PVCV) {
        croak("sv %p is not a function", sv);
    }
    switch(n) {
      case 0: flags = G_VOID; break;
      case 1: flags = G_SCALAR; break;
      default: flags = G_ARRAY; break;
    }

    ENTER;
    SAVETMPS;
    PUSHMARK(SP);
    // caller passing ownership of args, callee wants mortals
    while(*arg) {
        mXPUSHs(*arg++);
    }
    PUTBACK;
    count = call_sv(sv, G_EVAL | flags);
    SPAGAIN;
    SP -= count;
    ax = (SP - PL_stack_base) + 1;
    if(SvTRUE(ERRSV)) {
        err = newSVsv(ERRSV);
    } else {
        int i;
        for(i = 0; i < count && i < n; i++) {
            ret[i] = ST(i);
            // callee passes mortal rets, caller wants ownership
            SvREFCNT_inc(ret[i]);
        }
        err = NULL;
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    return err;
}

void glue_inc(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    SvREFCNT_inc(sv);
}

void glue_dec(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    SvREFCNT_dec(sv);
}

static int dbg_vtbl_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    PERL_SET_CONTEXT(my_perl);
    char buf[128];
    int l = write(STDERR_FILENO, buf, sprintf(buf, "SVFREE  %p\n", sv));

    (void) l;
    return 0;
}

static MGVTBL dbg_vtbl = { 0, 0, 0, 0, dbg_vtbl_sv_free };
void glue_track(pTHX_ SV *sv) {
    sv_magicext(sv, 0, PERL_MAGIC_ext, &dbg_vtbl, (char *)0xc0ffee, 0);
}

int glue_count_live(pTHX) {
    PERL_SET_CONTEXT(my_perl);
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

bool glue_getBool(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    return SvTRUE(sv);
}

IV glue_getIV(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    return SvIV(sv);
}

UV glue_getUV(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    return SvUV(sv);
}

NV glue_getNV(pTHX_ SV *sv) {
    PERL_SET_CONTEXT(my_perl);
    return SvNV(sv);
}

const char *glue_getPV(pTHX_ SV *sv, STRLEN *len) {
    PERL_SET_CONTEXT(my_perl);
    return SvPV(sv, *len);
}

bool glue_walkAV(pTHX_ SV *sv, IV data) {
    PERL_SET_CONTEXT(my_perl);
    if(SvROK(sv)) {
        AV *av = (AV *)SvRV(sv);
        if(SvTYPE((SV *)av) == SVt_PVAV) {
            I32 i = 0;
            SAVETMPS;
            SV **eltp;
            while((eltp = av_fetch(av, i++, 0)))
                goStepAV(data, *eltp);
            FREETMPS;
            return TRUE;
        }
    }
    return FALSE;
}

bool glue_walkHV(pTHX_ SV *sv, IV data) {
    PERL_SET_CONTEXT(my_perl);
    if(SvROK(sv)) {
        HV *hv = (HV *)SvRV(sv);
        if(SvTYPE((SV *)hv) == SVt_PVHV) {
            HE *he;
            SAVETMPS;
            hv_iterinit(hv);
            while((he = hv_iternext(hv)))
                goStepHV(data, HeSVKEY_force(he), HeVAL(he));
            FREETMPS;
            return TRUE;
        }
    }
    return FALSE;
}

SV *glue_newBool(pTHX_ bool v) {
    PERL_SET_CONTEXT(my_perl);
    return boolSV(v);
}

SV *glue_newIV(pTHX_ IV v) {
    PERL_SET_CONTEXT(my_perl);
    return newSViv(v);
}

SV *glue_newUV(pTHX_ UV v) {
    PERL_SET_CONTEXT(my_perl);
    return newSVuv(v);
}

SV *glue_newNV(pTHX_ NV v) {
    PERL_SET_CONTEXT(my_perl);
    return newSVnv(v);
}

SV *glue_newPV(pTHX_ char *str, STRLEN len) {
    PERL_SET_CONTEXT(my_perl);
    SV *rv = newSVpvn(str, len);
    free(str);
    return rv;
}

SV *glue_newAV(pTHX_ SV **elts) {
    PERL_SET_CONTEXT(my_perl);
    AV *av = newAV();
    while(*elts)
        av_push(av, *elts++);
    return newRV_noinc((SV *)av);
}

SV *glue_newHV(pTHX_ SV **elts) {
    PERL_SET_CONTEXT(my_perl);
    HV *hv = newHV();
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
    PERL_SET_CONTEXT(my_perl);
    return newRV_inc(sv);
}

/* glue_invoke() needs these details */
typedef struct {
    IV call;
    IV n_arg;
    IV n_ret;
} glue_cb_t;

/* When Perl releases our CV we should notify Go */
static int glue_vtbl_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    PERL_SET_CONTEXT(my_perl);
    glue_cb_t *cb = (glue_cb_t *)mg->mg_ptr;
    goRelease(cb->call);
    return 0;
}
static MGVTBL glue_vtbl = { 0, 0, 0, 0, glue_vtbl_sv_free };

/* XS stub for Go callbacks */
XS(glue_invoke)
{
    dXSARGS;
    MAGIC *mg;
    SV **arg, **ret;
    int i;

    mg = mg_findext((SV *)cv, PERL_MAGIC_ext, &glue_vtbl);
    glue_cb_t *cb = (glue_cb_t *)mg->mg_ptr;
    if (items != cb->n_arg)
        croak("expected %d args", cb->n_arg);
    // args are already mortals
    arg = alloca(cb->n_arg * sizeof(SV *));
    ret = alloca(cb->n_ret * sizeof(SV *));
    for(i = 0; i < cb->n_arg; i++)
        arg[i] = ST(i);
    goInvoke(cb->call, arg, ret);
    // rets must be mortalized on the way out
    for(i = 0; i < cb->n_ret; i++)
        ST(i) = sv_2mortal(ret[i]);
    XSRETURN(i);
}

/* Tie a CV to glue_invoke() and stash the Go details */
SV *glue_newCV(pTHX_ IV call, IV num_in, IV num_out) {
    PERL_SET_CONTEXT(my_perl);
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    glue_cb_t cb = { call, num_in, num_out };
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &glue_vtbl, (char *)&cb, sizeof(cb));
    return newRV_noinc((SV *)cv);
}
