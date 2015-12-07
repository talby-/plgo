
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
void glue_track(gPL, gSV);

IV glue_count_live(gPL);
gSV *glue_alloc(IV);

bool glue_getBool(gPL, gSV);
IV glue_getIV(gPL, gSV);
UV glue_getUV(gPL, gSV);
NV glue_getNV(gPL, gSV);
const char *glue_getPV(gPL, gSV, STRLEN *);

void glue_walkAV(gPL, gSV, UV);
void glue_walkHV(gPL, gSV, UV);

gSV glue_newBool(gPL, bool);
gSV glue_newIV(gPL, IV);
gSV glue_newUV(gPL, UV);
gSV glue_newNV(gPL, NV);
gSV glue_newPV(gPL, char *, STRLEN);
gSV glue_newAV(gPL, gSV *);
gSV glue_newHV(gPL, gSV *);
gSV glue_newCV(gPL, UV);
gSV glue_newRV(gPL, gSV);
gSV glue_newObj(gPL, UV, char *, char **);
