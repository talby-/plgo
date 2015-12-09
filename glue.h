
#define PERL_NO_GET_CONTEXT
#include "EXTERN.h"
#include "perl.h"

/* go1.5 has trouble with the definitions of PerlIntpreter and SV (see
 * https://github.com/golang/go/issues/13039 for details), so we are
 * unable to pass pointers to these types using cgo.  Instead we make
 * some fake opaque struct pointers and play casting games in each
 * handler function. */
typedef struct gPL *gPL;
typedef struct gSV *gSV;

gPL glue_init();

void glue_fini(gPL);

gSV glue_eval(gPL, char *, gSV *);
gSV glue_call_sv(gPL, gSV, gSV *, gSV *, IV);

void glue_inc(gPL, gSV);
void glue_dec(gPL, gSV);

IV glue_count_live(gPL);
gSV *glue_alloc(IV);
void glue_dump(gPL, gSV);

void glue_getBool(gPL, bool *, gSV);
void glue_getIV(gPL, IV *, gSV);
void glue_getUV(gPL, UV *, gSV);
void glue_getNV(gPL, NV *, gSV);
void glue_getPV(gPL, char **, STRLEN *, gSV);

void glue_walkAV(gPL, gSV, UV);
void glue_walkHV(gPL, gSV, UV);

void glue_setBool(gPL, gSV *, bool);
void glue_setIV(gPL, gSV *, IV);
void glue_setUV(gPL, gSV *, UV);
void glue_setNV(gPL, gSV *, NV);
void glue_setPV(gPL, gSV *, char *, STRLEN);
void glue_setAV(gPL, gSV *, gSV *);
void glue_setHV(gPL, gSV *, gSV *);
void glue_setCV(gPL, gSV *, UV);
void glue_setObj(gPL, gSV *, UV, char *, char **);
