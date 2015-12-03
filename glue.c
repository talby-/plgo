
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

/* silly cast helpers */
static inline SV *asSV(gSV plv) { return (SV *)plv; }
static inline gSV asgSV(SV *sv) { return (gSV)sv; }
static inline gSV *asgSVp(SV **psv) { return (gSV *)psv; }

gPL glue_init() {
    int argc = 3;
    char *argv[] = { "", "-e", "0", NULL };
    
    PerlInterpreter *my_perl;
    my_perl = perl_alloc();
    perl_construct(my_perl);
    perl_parse(my_perl, xs_init, argc, argv, NULL);
    PL_exit_flags |= PERL_EXIT_DESTRUCT_END;
    return (gPL)my_perl;
}

void glue_fini(gPL pl) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    perl_destruct(my_perl);
    perl_free(my_perl);
}

gSV glue_eval(gPL pl, char *text, gSV *errp) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    SV *rv;
    ENTER;
    SAVETMPS;
    rv = eval_pv(text, FALSE);
    if(SvTRUE(ERRSV)) {
        *errp = asgSV(newSVsv(ERRSV));
    } else {
        *errp = NULL;
    }
    SvREFCNT_inc(rv);
    FREETMPS;
    LEAVE;
    free(text);
    return asgSV(rv);
}

gSV glue_call_sv(gPL pl, gSV sv, gSV *arg, gSV *ret, IV n) {
    dTHXa(pl);
    I32 ax;
    int count;
    int flags;
    SV *err;
    dSP;

    PERL_SET_CONTEXT(my_perl);
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
        mXPUSHs(asSV(*arg++));
    }
    PUTBACK;
    count = call_sv(asSV(sv), G_EVAL | flags);
    SPAGAIN;
    SP -= count;
    ax = (SP - PL_stack_base) + 1;
    if(SvTRUE(ERRSV)) {
        err = newSVsv(ERRSV);
    } else {
        int i;
        for(i = 0; i < count && i < n; i++) {
            ret[i] = asgSV(ST(i));
            // callee passes mortal rets, caller wants ownership
            SvREFCNT_inc(ret[i]);
        }
        err = NULL;
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    return asgSV(err);
}

void glue_inc(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    SvREFCNT_inc(asSV(sv));
}

void glue_dec(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    SvREFCNT_dec(asSV(sv));
}

static int dbg_vtbl_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    PERL_SET_CONTEXT(my_perl);
    char buf[128];
    int l = write(STDERR_FILENO, buf, sprintf(buf, "SVFREE  %p\n", sv));

    (void) l;
    return 0;
}

static MGVTBL dbg_vtbl = { 0, 0, 0, 0, dbg_vtbl_sv_free };
void glue_track(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    sv_magicext(asSV(sv), 0, PERL_MAGIC_ext, &dbg_vtbl, (char *)0xc0ffee, 0);
}

IV glue_count_live(gPL pl) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    /* Devel::Leak proved to be too expensive to run during scans, so
     * this lifts a bit of it's algorithm for something to give us
     * simple live variable allocation counts */
    SV *sva;
    int i, n;
    IV rv = 0;
    for(sva = PL_sv_arenaroot; sva; sva = (SV *)SvANY(sva))
        for(i = 1, n = SvREFCNT(sva); i < n; i++)
            if(SvTYPE(sva + i) != SVTYPEMASK)
                rv++;
    return rv;
}

bool glue_getBool(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return SvTRUE(asSV(sv));
}

IV glue_getIV(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return SvIV(asSV(sv));
}

UV glue_getUV(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return SvUV(asSV(sv));
}

NV glue_getNV(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return SvNV(asSV(sv));
}

const char *glue_getPV(gPL pl, gSV sv, STRLEN *len) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return SvPV(asSV(sv), *len);
}

void glue_walkAV(gPL pl, gSV sv, UV data) {
    dTHXa(pl);
    SV **lst = NULL;
    I32 len = 0;

    PERL_SET_CONTEXT(my_perl);
    SAVETMPS;
    if(SvROK(asSV(sv))) {
        AV *av = (AV *)SvRV(asSV(sv));
        if(SvTYPE((SV *)av) == SVt_PVAV) {
            lst = AvARRAY(av);
            len = 1 + av_top_index(av);
        }
    }
    goList(data, asgSVp(lst), len);
    FREETMPS;
}

void glue_walkHV(gPL pl, gSV sv, UV data) {
    dTHXa(pl);
    SV **lst = NULL;
    IV len = 0;

    PERL_SET_CONTEXT(my_perl);
    SAVETMPS;
    if(SvROK(asSV(sv))) {
        HV *hv = (HV *)SvRV(asSV(sv));
        if(SvTYPE((SV *)hv) == SVt_PVHV) {
            HE *he;
            SV **p;
            lst = alloca(HvKEYS(hv) << 1);
            p = lst;
            hv_iterinit(hv);
            while((he = hv_iternext(hv))) {
                *p++ = HeSVKEY_force(he);
                *p++ = HeVAL(he);
            }
            len = p - lst;
        }
    }
    goList(data, asgSVp(lst), len);
    FREETMPS;
}

gSV glue_newBool(gPL pl, bool v) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return asgSV(boolSV(v));
}

gSV glue_newIV(gPL pl, IV v) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return asgSV(newSViv(v));
}

gSV glue_newUV(gPL pl, UV v) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return asgSV(newSVuv(v));
}

gSV glue_newNV(gPL pl, NV v) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return asgSV(newSVnv(v));
}

gSV glue_newPV(gPL pl, char *str, STRLEN len) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    SV *rv = newSVpvn(str, len);
    free(str);
    return asgSV(rv);
}

gSV glue_newAV(gPL pl, gSV *elts) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    AV *av = newAV();
    while(*elts)
        av_push(av, asSV(*elts++));
    return asgSV(newRV_noinc((SV *)av));
}

gSV glue_newHV(gPL pl, gSV *elts) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    HV *hv = newHV();
    while(*elts) {
        SV *k = asSV(*elts++);
        SV *v = asSV(*elts++);
        hv_store_ent(hv, k, v, 0);
        SvREFCNT_dec(k);
        // hv_store_ent has taken ownership of v
    }
    return asgSV(newRV_noinc((SV *)hv));
}

gSV glue_newRV(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    return asgSV(newRV_inc(asSV(sv)));
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
    goInvoke(cb->call, asgSVp(arg), asgSVp(ret));
    // rets must be mortalized on the way out
    for(i = 0; i < cb->n_ret; i++)
        ST(i) = sv_2mortal(ret[i]);
    XSRETURN(i);
}

/* Tie a CV to glue_invoke() and stash the Go details */
gSV glue_newCV(gPL pl, UV call, IV num_in, IV num_out) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    glue_cb_t cb = { call, num_in, num_out };
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &glue_vtbl, (char *)&cb, sizeof(cb));
    return asgSV(newRV_noinc((SV *)cv));
}
