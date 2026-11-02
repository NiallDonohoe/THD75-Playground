# 0010 — Our own SGP4, near-Earth only

Date: 2026-10-30 · Status: accepted

The `orbit` package implements SGP4 itself, in pure Go, rather than
depending on a Go SGP4 library. That keeps 0001 (standard library only),
as 0006 and 0008 did for APRS and FT8. It follows Vallado, Crawford, Hujsak
and Kelso, "Revisiting Spacetrack Report #3" (AIAA 2006-6753), with WGS-72
constants, as element sets are fitted with.

Only the near-Earth branch is implemented: orbits with a period under 225
minutes. That covers every low-orbit amateur satellite the D75 can hear.
The deep-space branch (SDP4: lunar and solar perturbations and resonance)
would roughly double the code, for high orbits the radio mostly cannot
hear. Deep-space element sets are refused with `ErrDeepSpace` and shown on
the page as "not predicted". The owner chose this on 30 Oct 2026.

Verification uses the paper's own test data: `SGP4-VER.TLE` and
`tcppver.out`, as distributed with the paper and with python-sgp4 (MIT).
All nine near-Earth cases (158 rows) agree to the 1 mm and 1 mm/s the file
prints. Three rows of each are embedded in the tests, with the source
cited.
