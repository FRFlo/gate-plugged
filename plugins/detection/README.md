# Gate detection parity

The Gate plugin follows the HackedServer detection resources and implements
the passive client checks available through Gate's public events. This includes
`meteor-client` generic detection, client brand checks, Fabric channel checks,
and the `spoofed_brand` check (a `vanilla`/`minecraft` brand combined with
Fabric loader channels).

HackedServer's active sign-translation probes for Meteor, Wurst, and Freecam
are intentionally not enabled here. Gate v0.73.13 exposes packet writing but
does not expose a public plugin hook for inbound `UPDATE_SIGN` packets or raw
player-position packets. Implementing those probes would require patching or
forking Gate; this repository does not silently simulate them.
