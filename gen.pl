#!/usr/bin/perl
use strict;
use warnings;
use ExtUtils::Embed ();

sub trim {
    my($s) = @_;
    $s =~ s/^\s+|\s+$//sg;
    return $s;
}

my $hdr = qq{/*
#cgo CFLAGS: ${\ trim(ExtUtils::Embed::ccopts()) }
#cgo LDFLAGS: ${\ trim(ExtUtils::Embed::ldopts()) }
#include "glue.h"
*/
import "C"};

open my($src), '+<', 'link.go';
my $txt = do {
    undef $/;
    <$src>;
};
$txt =~ s{/\*(.*?)\*/\s*import\s+"C"}{$hdr}s
    or die "unable to match cgo block in link.go";
seek $src, 0, 0;
print $src $txt;
use bytes;
truncate $src, length($txt);
