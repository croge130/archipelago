// Package routere2e holds only tests: the router exercised over the real
// websocket backend, end to end, once. It is a separate module so that
// router itself depends on no backend, and so that no module depends on
// router merely for the sake of its tests. Nothing here is imported.
package routere2e
