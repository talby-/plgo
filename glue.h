
#include "EXTERN.h"
#include "perl.h"

PerlInterpreter *glue_init();

void glue_fini(pTHX);

SV *glue_eval(pTHX_ char *, SV **);
SV *glue_call_sv(pTHX_ SV *, SV **, SV **, int);
SV *glue_call_method(pTHX_ char *, SV **, SV **);

void glue_inc(pTHX_ SV *);
void glue_dec(pTHX_ SV *);

SV *glue_undef(pTHX);
void glue_sv_dump(pTHX_ SV *);
bool glue_SvOK(pTHX_ SV *);
int glue_count_live(pTHX);

bool glue_getBool(pTHX_ SV *);
IV glue_getIV(pTHX_ SV *);
UV glue_getUV(pTHX_ SV *);
NV glue_getNV(pTHX_ SV *);
const char *glue_getPV(pTHX_ SV *, STRLEN *);

extern void glue_stepAV(void *, SV *);
bool glue_walkAV(pTHX_ SV *, void *);

extern void glue_stepHV(void *, SV *, SV *);
bool glue_walkHV(pTHX_ SV *, void *);

SV *glue_newBool(pTHX_ bool);
SV *glue_newIV(pTHX_ IV);
SV *glue_newUV(pTHX_ UV);
SV *glue_newNV(pTHX_ NV);
SV *glue_newPV(pTHX_ char *, STRLEN);
SV *glue_newAV(pTHX_ SV **);
SV *glue_newHV(pTHX_ SV **);
SV *glue_newCV(pTHX_ void *, void *);
SV *glue_newRV(pTHX_ SV *);
