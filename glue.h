
#include "EXTERN.h"
#include "perl.h"

PerlInterpreter *glue_init();

void glue_fini(pTHX);

SV *glue_eval(pTHX_ char *, SV **);
SV *glue_call_method(pTHX_ char *, SV **, SV **);

void glue_inc(pTHX_ SV *);
void glue_dec(pTHX_ SV *);

SV *glue_undef(pTHX);

bool glue_getBool(pTHX_ SV *);
IV glue_getIV(pTHX_ SV *);
UV glue_getUV(pTHX_ SV *);
NV glue_getNV(pTHX_ SV *);
const char *glue_getPV(pTHX_ SV *, STRLEN *);

SV *glue_newBool(pTHX_ bool);
SV *glue_newIV(pTHX_ IV);
SV *glue_newUV(pTHX_ UV);
SV *glue_newNV(pTHX_ NV);
SV *glue_newPV(pTHX_ char *, STRLEN);
SV *glue_newAV(pTHX_ SV **);
SV *glue_newHV(pTHX_ SV **);
SV *glue_newCV(pTHX_ void *, void *);

