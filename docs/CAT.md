# CAT commands

The commands this program sends, as LA3QMA's TH-D74 reference (firmware
V1.10) describes them. **None is verified on a TH-D75 yet.** After the M0
probe runs, each row gets the TH-D75's actual reply and the firmware version
it was seen on (`cat.reads` in probe-report.json).

Wire format: ASCII, command, a space, comma-separated parameters, `\r`.
Errors: `?` (unknown or malformed), `N` (not available now). Band B is
parameter `1`.

| Command | Read | Reply (D74 reference) | Write (allowed) | Verified on TH-D75 |
| --- | --- | --- | --- | --- |
| ID | `ID` | `ID TH-D74` | — | pending |
| FV | `FV` | `FV 1.08` (format may have more fields) | — | pending |
| FQ | `FQ 1` | `FQ 1,0007074000` (10-digit Hz) | `FQ 1,<10 digits>`, 100 000 – 524 000 000 Hz | pending |
| FO | `FO 1` | `FO 1,` + 20 more fields (frequency, offset, steps, mode, fine mode, fine step, tones, D-STAR) | — never written | pending |
| MD | `MD 1` | `MD 1,4` | `MD 1,<m>`, m ∈ {0 FM, 2 AM, 3 LSB, 4 USB, 5 CW, 6 NFM, 8 WFM, 9 R-CW}; DV (1) and DR (7) refused | pending |
| IO | `IO` | `IO 1` (0 AF/Off, 1 IF, 2 Detect) | `IO 0..2` | pending |
| FT | `FT` | `FT 0` | `FT 0` / `FT 1` (no band parameter) | pending |
| FS | `FS` | `FS 0` (0 = 20, 1 = 100, 2 = 500, 3 = 1000 Hz) | `FS 0..3` (no band parameter) | pending |
| SH | `SH 0`, `SH 1`, `SH 2` | `SH 0,2` (index into SSB/CW/AM width tables) | — read only | pending |
| BY | `BY 1` | `BY 1,0` (0 closed, 1 open) | — | pending |
| AI | `AI` | `AI 0` | `AI 0` / `AI 1` | pending |

Unsolicited lines with AI 1: format unknown (Q16). The fake radio assumes
`FQ 1,<10 digits>`; the session routes any line that does not answer the
command in flight as an event, whatever it looks like.

## Hard-blocked

Refused in `radio.Check` before anything reaches the port, in both the typed
path and the console path, case-insensitively, with a test each: TX, BE, PT,
TN, AS, 0M (PROGRAM), 0G (KENWOOD), SR, PS, ME, MR, DW, UP, BT, PC. Every
other command not in the table above is refused too.
