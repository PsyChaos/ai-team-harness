// Package scheduler builds deterministic eligible frontiers from exact snapshots.
// Callers supply verified state, compatible provider names and active leases;
// the package neither queries GitHub nor selects providers nor executes work.
//
// Review and recovery precede new implementation at normal priority. After L
// consecutive eligible rounds waiting, work enters a FIFO promoted tier above
// all normal priorities. Ages saturate at L. With N items ahead in FIFO order when an
// item arrives (same-round arrivals sort by ID), it is dispatched by at most
// round L+N+1 (arrival is round 1),
// provided it stays eligible and each round successfully dispatches the first
// frontier item. Later arrivals cannot overtake promoted work. Capacity outages,
// failed dispatches and restarts cannot provide a wall-clock starvation bound.
// Call Dispatched on success; refresh leases/capacity before the next selection.
package scheduler
