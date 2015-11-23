#!/usr/bin/perl
use strict;
use warnings;
use ExtUtils::Embed ();

sub trim {
    my($s) = @_;
    $s =~ s/^\s+|\s+$//sg;
    return $s;
}

my($fn) = @ARGV;

my $hdr = qq{/*
#cgo CFLAGS: -Wall ${\ trim(ExtUtils::Embed::ccopts()) }
#cgo LDFLAGS: ${\ trim(ExtUtils::Embed::ldopts()) }
#include "glue.h"
*/
import "C"};

open my($src), '+<', $fn or die "unable to open $fn";
my $txt = do {
    local $/;
    <$src>;
};
$txt =~ s{/\*(.*?)\*/\s*import\s+"C"}{$hdr}s
    or die "unable to find cgo block in $fn";
seek $src, 0, 0;
print $src $txt;
use bytes;
truncate $src, length($txt);
