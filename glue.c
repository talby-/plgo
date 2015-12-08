
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

/* A little macro nuttiness to get Perl errors report the correct file
 * and line. */
#define STRINGY_FLAT(x) #x
#define STRINGY(x) STRINGY_FLAT(x)
static const char *perl_runtime =
    "#line " STRINGY(__LINE__) " \"" __FILE__ "\" \n\n\
    use strict; \n\
    use warnings; \n\
    package Go { \n\
        sub type { \n\
            my($tgt) = @_; \n\
            $tgt =~ s|/|::|sg; \n\
            $tgt = qq(Go::$tgt); \n\
            eval qq( \n\
                package $tgt; \n\
                use base 'Go::Pxy'; \n\
            ) unless UNIVERSAL::isa($tgt, qq(Go::Pxy)); \n\
            return $tgt; \n\
        } \n\
    } \n\
    package Go::Pxy { \n\
        # empty for now \n\
    } \n\
";

typedef struct {
    UV st_id;
    char *st_fname;
} glue_st_t;

static int vtbl_st_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    PERL_SET_CONTEXT(my_perl);
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    goReleaseST(st->st_id);
    if(st->st_fname)
        free(st->st_fname);
    return 0;
}

static int vtbl_stf_getf(pTHX_ SV *sv, MAGIC *mg) {
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    sv_setsv(sv, asSV(goSTGetf(st->st_id, st->st_fname)));
    return 0;
}

static int vtbl_stf_setf(pTHX_ SV *sv, MAGIC *mg) {
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    goSTSetf(st->st_id, st->st_fname, asgSV(sv));
    return 0;
}

static MGVTBL vtbl_st = {
    0,
    0,
    0,
    0,
    vtbl_st_sv_free,
};
static MGVTBL vtbl_stf = {
    vtbl_stf_getf,
    vtbl_stf_setf,
    0,
    0,
    vtbl_st_sv_free,
};

XS(glue_autoload) {
    dXSARGS;
    MAGIC *mg;
    SV **arg, **ret;
    int i;

    STRLEN l;
    char *name = SvPV(get_sv("Go::Pxy::AUTOLOAD", FALSE), l);

    while(--l > 0) {
        if(name[l] != ':' || name[l + 1] != ':')
            continue;
        l += 2;
        break;
    }
    name += l;

    /* steal the first argument, this is our proxy */
    SV *self = ST(0);
    mg = mg_findext((SV *)self, PERL_MAGIC_ext, &vtbl_st);
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    // args are already mortals
    arg = alloca(items * sizeof(SV *));
    for(i = 0; i < items - 1; i++)
        arg[i] = ST(i + 1);
    arg[i] = NULL;
    ret = (SV **)goSTCall(st->st_id, name, asgSVp(arg));
    /* rets must be mortalized on the way out */
    for(i = 0; ret[i]; i++)
        ST(i) = sv_2mortal(ret[i]);
    free(ret);
    XSRETURN(i);
}

gPL glue_init() {
    int argc = 3;
    char *argv[] = { "", "-e", "0", NULL };
    
    PerlInterpreter *my_perl;
    my_perl = perl_alloc();
    perl_construct(my_perl);
    perl_parse(my_perl, xs_init, argc, argv, NULL);
    PL_exit_flags |= PERL_EXIT_DESTRUCT_END;
    eval_pv(perl_runtime, TRUE);
    newXS("Go::Pxy::AUTOLOAD", glue_autoload, __FILE__);
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

gSV *glue_alloc(IV n) {
    return (gSV *)calloc(n, sizeof(SV *));
}

void glue_dump(gPL pl, gSV sv) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    sv_dump(asSV(sv));
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
    IV len = 0;
    SV **lst = NULL;

    PERL_SET_CONTEXT(my_perl);
    SAVETMPS;
    if(SvROK(asSV(sv))) {
        HV *hv = (HV *)SvRV(asSV(sv));
        if(SvTYPE((SV *)hv) == SVt_PVHV) {
            HE *he;
            IV i = 0;
            len = HvKEYS(hv) << 1;
            lst = alloca(len * sizeof(SV *));
            hv_iterinit(hv);
            while((he = hv_iternext(hv))) {
                lst[i++] = HeSVKEY_force(he);
                lst[i++] = HeVAL(he);
            }
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

/* When Perl releases our CV we should notify Go */
static int vtbl_cb_sv_free(pTHX_ SV *sv, MAGIC *mg) {
    PERL_SET_CONTEXT(my_perl);
    UV id = (UV)mg->mg_ptr;
    goReleaseCB(id);
    return 0;
}
static MGVTBL vtbl_cb = { 0, 0, 0, 0, vtbl_cb_sv_free };

/* XS stub for Go callbacks */
XS(glue_invoke)
{
    dXSARGS;
    MAGIC *mg;
    SV **arg, **ret;
    int i;

    mg = mg_findext((SV *)cv, PERL_MAGIC_ext, &vtbl_cb);
    UV id = (UV)mg->mg_ptr;

    // args are already mortals
    arg = alloca((items + 1) * sizeof(SV *));
    for(i = 0; i < items; i++)
        arg[i] = ST(i);
    arg[i] = NULL;

    // rets must be mortalized on the way out
    ret = (SV **)goInvoke(id, asgSVp(arg));
    for(i = 0; ret[i]; i++)
        ST(i) = sv_2mortal(ret[i]);
    free(ret);
    XSRETURN(i);
}

/* Tie a CV to glue_invoke() and stash the Go details */
gSV glue_newCV(gPL pl, UV id) {
    dTHXa(pl);
    PERL_SET_CONTEXT(my_perl);
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &vtbl_cb, (char *)id, 0);
    return asgSV(newRV_noinc((SV *)cv));
}

gSV glue_newObj(gPL pl, UV id, char *gotype, char **attrs) {
    /* this is going to be kind of long... */
    dTHXa(pl);
    //dSP;
    HV *hv;
    SV *sv;
    glue_st_t st;
    PERL_SET_CONTEXT(my_perl);

    SAVETMPS;
    hv = newHV();
    sv = newRV_noinc((SV *)hv);

    //ENTER;
    //PUSHMARK(SP);
    //mXPUSHs(newSVpv(gotype, 0));
    //mXPUSHu(id);
    //PUTBACK;
    //call_pv("Go::type", G_EVAL | G_SCALAR);
    //SPAGAIN;
    //sv_bless(sv, gv_stashsv(POPs, GV_ADD));
    //PUTBACK;
    //LEAVE;

    sv_bless(sv, gv_stashpv("Go::Pxy", GV_ADD));

    st.st_id = id;
    st.st_fname = NULL;
    sv_magicext(sv, 0, PERL_MAGIC_ext, &vtbl_st, (char *)&st, sizeof(st));
    free(gotype);

    while(*attrs) {
        /* fill in field stubs */
        SV *v = newSV(0);
        st.st_fname = *attrs;
        sv_magicext(v, 0, PERL_MAGIC_ext, &vtbl_stf, (char *)&st, sizeof(st));
        hv_store(hv, *attrs, 0 - strlen(*attrs), v, 0);
        // hv_store has taken ownership of v
        attrs++;
    }
    /* slots in the hv are filled, now lock it */
    SvREADONLY_on((SV *)hv);
    FREETMPS;
    return asgSV(sv);
}
