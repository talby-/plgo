#!/usr/bin/env perl
use strict;
use warnings;
use ExtUtils::Embed ();

sub trim {
    my($s) = @_;
    $s =~ s/^\s+|\s+$//sg;
    return $s;
}

my($fn) = @ARGV;

ExtUtils::Embed::xsinit(undef, undef, []);
# CVE-2018-6574: cgo no longer allows some linker flags
my $ccopts = trim(ExtUtils::Embed::ccopts()) =~ s/-fwrapv\s*//sgr;
my $ldopts = trim(ExtUtils::Embed::ldopts()) =~ s/-[Wf]\S*\s*//sgr;

my $hdr = qq{/*
#cgo CFLAGS: -Wall $ccopts
#cgo LDFLAGS: $ldopts
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
