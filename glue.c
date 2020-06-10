
#include "glue.h"
#include "XSUB.h"
#include "_cgo_export.h"

/* notes: pretty much any char* coming from Go is going to callee
 * allocated and caller freed so that the go side isn't peppered with
 * tons of defer calls */

void xs_init (pTHX); /* provided by perlxsi.c */

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
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    goReleaseST(st->st_id);
    if(st->st_fname)
        free(st->st_fname);
    return 0;
}

static int vtbl_stf_getf(pTHX_ SV *sv, MAGIC *mg) {
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    sv_setsv(sv, goSTGetf(st->st_id, st->st_fname));
    return 0;
}

static int vtbl_stf_setf(pTHX_ SV *sv, MAGIC *mg) {
    glue_st_t *st = (glue_st_t *)mg->mg_ptr;
    goSTSetf(st->st_id, st->st_fname, sv);
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
    ret = (SV **)goSTCall(st->st_id, name, arg);
    /* rets must be mortalized on the way out */
    for(i = 0; ret[i]; i++)
        ST(i) = sv_2mortal(ret[i]);
    free(ret);
    XSRETURN(i);
}

tTHX glue_init() {
    int argc = 3;
    char *argv[] = { "", "-e", "0", NULL };
    
    PerlInterpreter *my_perl;
    my_perl = perl_alloc();
    perl_construct(my_perl);
    perl_parse(my_perl, xs_init, argc, argv, NULL);
    PL_exit_flags |= PERL_EXIT_DESTRUCT_END;
    eval_pv(perl_runtime, TRUE);
    newXS("Go::Pxy::AUTOLOAD", glue_autoload, __FILE__);
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

SV *glue_call_sv(pTHX_ SV *sv, SV **arg, SV **ret, UV n) {
    I32 ax;
    I32 count;
    dSP;
    int flags;
    SV *err;
    UV i = 0;

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
        while(i < count && i < n) {
            ret[i] = ST(i);
            // callee passes mortal rets, caller wants ownership
            SvREFCNT_inc(ret[i]);
            i++;
        }
        err = NULL;
    }
    PUTBACK;
    FREETMPS;
    LEAVE;
    if(i < n)
        memset(ret + i, '\0', sizeof(SV *) * (n - i));
    return err;
}

void glue_inc(pTHX_ SV *sv) {
    SvREFCNT_inc(sv);
}

void glue_dec(pTHX_ SV *sv) {
    /* Go might hand us a null ptr because we sometimes return a null in
     * place of ERRSV to mean no error occured. */
    if(!sv)
        return;
    SvREFCNT_dec(sv);
}

IV glue_count_live(pTHX) {
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

SV **glue_alloc(IV n) {
    return (SV **)calloc(n, sizeof(SV *));
}

void glue_dump(pTHX_ SV *sv) {
    sv_dump(sv);
}

void glue_getBool(pTHX_ bool *dst, SV *sv) {
    *dst = SvTRUE(sv);
}

void glue_getIV(pTHX_ IV *dst, SV *sv) {
    *dst = SvIV(sv);
}

void glue_getUV(pTHX_ UV *dst, SV *sv) {
    *dst = SvUV(sv);
}

void glue_getNV(pTHX_ NV *dst, SV *sv) {
    *dst = SvNV(sv);
}

void glue_getPV(pTHX_ char **dst, STRLEN *len, SV *sv) {
    *dst = SvPV(sv, *len);
}

void glue_walkAV(pTHX_ SV *sv, UV data) {
    SV **lst = NULL;
    I32 len = -1;

    SAVETMPS;
    if(SvROK(sv)) {
        AV *av = (AV *)SvRV(sv);
        if(SvTYPE((SV *)av) == SVt_PVAV) {
            lst = AvARRAY(av);
            len = 1 + av_top_index(av);
        }
    }
    goList(data, lst, len);
    FREETMPS;
}

void glue_walkHV(pTHX_ SV *sv, UV data) {
    IV len = -1;
    SV **lst = NULL;

    SAVETMPS;
    if(SvROK(sv)) {
        HV *hv = (HV *)SvRV(sv);
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
    goList(data, lst, len);
    FREETMPS;
}

void glue_setBool(pTHX_ SV **ptr, bool v) {
    if(!*ptr) *ptr = newSV(0);
    SvSetSV(*ptr, boolSV(v));
}

void glue_setIV(pTHX_ SV **ptr, IV v) {
    if(!*ptr) *ptr = newSV(0);
    sv_setiv(*ptr, v);
}

void glue_setUV(pTHX_ SV **ptr, UV v) {
    if(!*ptr) *ptr = newSV(0);
    sv_setuv(*ptr, v);
}

void glue_setNV(pTHX_ SV **ptr, NV v) {
    if(!*ptr) *ptr = newSV(0);
    sv_setnv(*ptr, v);
}

void glue_setPV(pTHX_ SV **ptr, char *str, STRLEN len) {
    if(!*ptr) *ptr = newSV(len);
    sv_setpvn(*ptr, str, len);
    free(str);
}

static inline void setRV(pTHX_ SV **ptr, SV *elt) {
    if(!*ptr) *ptr = newSV_type(SVt_IV);
    SvRV_set(*ptr, elt);
    SvROK_on(*ptr);
}

void glue_setAV(pTHX_ SV **ptr, SV **lst) {
    AV *av = newAV();
    while(*lst)
        av_push(av, *lst++);
    setRV(aTHX_ (SV **)ptr, (SV *)av);
}

void glue_setHV(pTHX_ SV **ptr, SV **lst) {
    HV *hv = newHV();
    while(*lst) {
        SV *k = *lst++;
        SV *v = *lst++;
        hv_store_ent(hv, k, v, 0);
        SvREFCNT_dec(k);
        // hv_store_ent has taken ownership of v
    }
    setRV(aTHX_ (SV **)ptr, (SV *)hv);
}

/* When Perl releases our CV we should notify Go */
static int vtbl_cb_sv_free(pTHX_ SV *sv, MAGIC *mg) {
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
    ret = (SV **)goInvoke(id, arg);
    for(i = 0; ret[i]; i++)
        ST(i) = sv_2mortal(ret[i]);
    free(ret);
    XSRETURN(i);
}

/* Tie a CV to glue_invoke() and stash the Go details */
void glue_setCV(pTHX_ SV **ptr, UV id) {
    CV *cv = newXS(NULL, glue_invoke, __FILE__);
    sv_magicext((SV *)cv, 0, PERL_MAGIC_ext, &vtbl_cb, (char *)id, 0);
    setRV(aTHX_ (SV **)ptr, (SV *)cv);
}

void glue_setObj(pTHX_ SV **ptr, UV id, char *gotype, char **attrs) {
    /* this is going to be kind of long... */
    //dSP;
    HV *hv;
    SV *sv;
    glue_st_t st;

    SAVETMPS;
    hv = newHV();
    setRV(aTHX_ (SV **)ptr, (SV *)hv);
    sv = *ptr;

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
}

bool glue_getId(pTHX_ SV *sv, UV *id, const char *kind) {
    MAGIC *mg;
    if(strcmp(kind, "func") == 0) {
        SV *cv = SvRV(sv);
        if(!SvMAGICAL(cv))
            return FALSE;
        if(!(mg = mg_findext(cv, PERL_MAGIC_ext, &vtbl_cb)))
            return FALSE;
        *id = (UV)mg->mg_ptr;
        return TRUE;
    }
    if(strcmp(kind, "struct") == 0) {
        if(!SvMAGICAL(sv))
            return FALSE;
        if(!(mg = mg_findext(sv, PERL_MAGIC_ext, &vtbl_st)))
            return FALSE;
        glue_st_t *st = (glue_st_t *)mg->mg_ptr;
        *id = st->st_id;
        return TRUE;
    }
    croak("Unsupported kind %s", kind);
}

void glue_setContext(pTHX) {
    PERL_SET_CONTEXT(my_perl);
}
