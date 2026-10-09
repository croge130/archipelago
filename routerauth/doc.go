// Package routerauth joins the router to Gatehouse-core
// (docs/architecture/21-router-and-handshake-model.md, "Authorization and
// the endpoint registry"). Its one idea: registering a route, the
// permission it requires and the advertised endpoint is a single call, so
// "which endpoints exist" and "what each needs" cannot drift apart — the
// failure 04 records for Lighthouse's adminTokenMessageTypes.
//
// The router itself knows nothing of permissions; a node that wants raw
// routing with its own authorization uses router alone.
package routerauth
