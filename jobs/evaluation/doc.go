// Package evaluation is the jobs base's pure logic: which state
// transitions are legal, how long to wait before a retry, how a job's
// parameters are validated and fingerprinted, and what a task kind's
// declared scope resolves to for one job. Nothing here touches a
// database — the storage layer's single-statement operations decide the
// same questions in SQL, and the integration tests check the two agree.
package evaluation
