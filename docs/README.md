# Documentation

- [index.md](index.md) — the site's home page: what tabula is, downloads.
- [PLAN.md](PLAN.md) — goals, architecture and milestones.
- [gui.md](gui.md) — the pages of the tabula GUI.
- [recording.md](recording.md) — recording a show (console and audio), replays, exports.
- [access.md](access.md) — the engineer password, musicians' in-ear links, viewers.
- [emulator.md](emulator.md) — what `tabula-emu` emulates and how.
- [API.md](API.md) — the tabula client's HTTP API.
- [testing-with-editors.md](testing-with-editors.md) — connecting X32-Edit / M32-Edit to the emulator.

## Reference material (not in the repository)

The protocol reference is Patrick-Gilles Maillot's *Unofficial X32/M32 OSC
Remote Protocol* (updated 2021-06-29), available from his site:
<https://sites.google.com/site/patrickmaillot/x32>.

Keep a local copy in `docs/reference/` (git-ignored), for instance:

```sh
mkdir -p docs/reference
# download the PDF from the link on the page above, then:
pdftotext -layout docs/reference/osc-protocol-maillot.pdf docs/reference/osc-protocol-maillot.txt
```
