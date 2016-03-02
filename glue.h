
#define PERL_NO_GET_CONTEXT
#include "EXTERN.h"
#include "perl.h"

tTHX glue_init();

void glue_fini(pTHX);

SV *glue_eval(pTHX_ char *, SV **);
SV *glue_call_sv(pTHX_ SV *, SV **, SV **, IV);

void glue_inc(pTHX_ SV *);
void glue_dec(pTHX_ SV *);

IV glue_count_live(pTHX);
SV **glue_alloc(IV);
void glue_dump(pTHX_ SV *);

void glue_getBool(pTHX_ bool *, SV *);
void glue_getIV(pTHX_ IV *, SV *);
void glue_getUV(pTHX_ UV *, SV *);
void glue_getNV(pTHX_ NV *, SV *);
void glue_getPV(pTHX_ char **, STRLEN *, SV *);

void glue_walkAV(pTHX_ SV *, UV);
void glue_walkHV(pTHX_ SV *, UV);

void glue_setBool(pTHX_ SV **, bool);
void glue_setIV(pTHX_ SV **, IV);
void glue_setUV(pTHX_ SV **, UV);
void glue_setNV(pTHX_ SV **, NV);
void glue_setPV(pTHX_ SV **, char *, STRLEN);
void glue_setAV(pTHX_ SV **, SV **);
void glue_setHV(pTHX_ SV **, SV **);
void glue_setCV(pTHX_ SV **, UV);
void glue_setObj(pTHX_ SV **, UV, char *, char **);
