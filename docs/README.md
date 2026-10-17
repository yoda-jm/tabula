# Documentation

- [PLAN.md](PLAN.md) — goals, architecture and milestones.

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
