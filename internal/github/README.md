# Provider adapter

Owns explicit go-gh authentication, bounded provider requests, immutable run identities and private reconciliation metadata. Tests inject synthetic transports and perform no hosted writes.

Queueing uses native repository concurrency and coordinator capacity discipline.
A dispatch response without a returned exact run ID is unknown and triggers no
search/retry. `Status` (also exposed as `Wait`) reads one attempt-specific snapshot.
`Cancel` sends at most one POST with bound before/after reads and one private
outcome; drift, lost acknowledgement or nonterminal confirmation stays unknown.
The immutable receipt and full external job census remain mandatory.
